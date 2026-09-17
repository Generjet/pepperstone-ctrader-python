import argparse
import datetime
import json
import re
import sys
import time
import uuid

from twisted.internet import reactor

from ctrader_fix import (
    Client,
    LogonRequest,
    NewOrderSingle,
    SecurityListRequest,
    MarketDataRequest,
)
from ctrader_fix.fixProtocol import FixProtocol
from ctrader_fix.messages import ResponseMessage

import trade_log

trade_log.configure("trade_ctrader_fix")


QUOTE_PORT = 5201
TRADE_PORT = 5202


def _patched_dataReceived(self, data):
    if not hasattr(self, "_buffer"):
        self._buffer = ""
    self._buffer += data.decode("ascii")
    delimiter = self.factory.delimiter
    pattern = re.compile(re.escape(delimiter) + r"10=\d{3}" + re.escape(delimiter))
    while True:
        match = pattern.search(self._buffer)
        if not match:
            break
        raw = self._buffer[: match.end()]
        self._buffer = self._buffer[match.end():]
        self.factory.received(ResponseMessage(raw, delimiter))


FixProtocol.dataReceived = _patched_dataReceived


def lots_to_units(symbol, lots):
    upper = str(symbol).upper()
    if upper in ("BTCUSD", "ETHUSD"):
        return int(float(lots))
    if upper in ("XAUUSD", "XAUEUR"):
        return int(float(lots) * 100)
    if upper in ("XAGUSD", "XAGEUR"):
        return int(float(lots) * 5000)
    if upper in ("ADAUSD", "XRPUSD", "DOGEUSD", "SOLUSD", "AVAXUSD",
                 "DOTUSD", "LINKUSD", "MATICUSD", "SHIBUSD", "LTCUSD",
                 "BCHUSD", "UNIUSD", "AAVEUSD", "ATOMUSD", "FILUSD"):
        return int(float(lots) * 100)
    return int(float(lots) * 100000)


def load_config(path):
    with open(path) as handle:
        cfg = json.load(handle)
    cfg.setdefault("TargetSubID", cfg["SenderSubID"])
    cfg.setdefault("Username", str(cfg["SenderCompID"]).rsplit(".", 1)[-1])
    return cfg


class SpotMarketDataRequest(MarketDataRequest):
    MDEntryTypes = (0, 1)

    def _getBody(self):
        fields = []
        fields.append(f"262={self.MDReqID}")
        fields.append(f"263={self.SubscriptionRequestType}")
        fields.append(f"264={self.MarketDepth}")
        if hasattr(self, "MDUpdateType"):
            fields.append(f"265={self.MDUpdateType}")
        fields.append(f"267={self.NoMDEntryTypes}")
        for entry_type in self.MDEntryTypes:
            fields.append(f"269={entry_type}")
        fields.append(f"146={self.NoRelatedSym}")
        fields.append(f"55={self.Symbol}")
        return f"{self.delimiter.join(fields)}"


class Bot:
    def __init__(self, args, quote_cfg, trade_cfg):
        self.args = args
        self.quote_cfg = quote_cfg
        self.trade_cfg = trade_cfg
        self.symbols = {}
        self.symbol_info = None
        self.resolved_name = None
        self.bid = None
        self.ask = None
        self.quote_logged = False
        self.trade_logged = False
        self.sec_requested = False
        self.symbol_requested = False
        self.order_placed = False
        self.position_id = None
        self.sl_sent = False
        self.tp_sent = False
        self.cl_ord_id = None
        self.sl_cl_ord_id = None
        self.tp_cl_ord_id = None
        self.sl_acked = False
        self.tp_acked = False
        self.filled = False
        self.units = None
        self._finished = False
        self.exit_code = 1
        self.quote_client = None
        self.trade_client = None

    def on_connected(self, client):
        cfg = self.quote_cfg if client is self.quote_client else self.trade_cfg
        logon = LogonRequest(cfg)
        logon.ResetSeqNum = True
        print(f"[CONNECT] {cfg['SenderSubID']} session connected, sending Logon")
        client.send(logon).addErrback(self.on_send_error)

    def on_disconnected(self, client, reason):
        print(f"[DISCONNECT] {reason}")
        if not self._finished:
            self.finish(1)

    def on_send_error(self, failure):
        print(f"[SEND ERROR] {failure}")
        if not self._finished:
            self.finish(1)
        return None

    def on_message(self, client, message):
        msg_type = message.getFieldValue(35)
        if msg_type == "A":
            if client is self.quote_client:
                self.quote_logged = True
                print("[LOGON] Quote session OK")
                self.request_security_list()
            elif client is self.trade_client:
                self.trade_logged = True
                print("[LOGON] Trade session OK")
        elif msg_type == "y" and client is self.quote_client:
            self.parse_security_list(message)
        elif msg_type == "W" and client is self.quote_client:
            self.parse_market_data(message)
        elif msg_type == "Y":
            print(f"[ERROR] Market data rejected: {message.getFieldValue(58)}")
            self.finish(1)
        elif msg_type == "8" and client is self.trade_client:
            self.parse_execution_report(message)
        elif msg_type in ("3", "9", "j"):
            print(f"[ERROR] {message.getFieldValue(58) or message.getMessage()}")
            self.finish(1)

    def request_security_list(self):
        if self.sec_requested or not self.quote_logged:
            return
        self.sec_requested = True
        req = SecurityListRequest(self.quote_cfg)
        req.SecurityReqID = "syms-1"
        req.SecurityListRequestType = 0
        print("[SYMBOL] Requesting security list")
        self.quote_client.send(req).addErrback(self.on_send_error)

    def parse_security_list(self, message):
        names = message.getFieldValue(1007)
        if not names:
            if message.getFieldValue(146) == "0":
                pass
            return
        symbol_ids = message.getFieldValue(55)
        digits = message.getFieldValue(1008)
        if not isinstance(names, list):
            names = [names]
            symbol_ids = [symbol_ids]
            digits = [digits]
        for name, sym_id, dig in zip(names, symbol_ids, digits):
            self.symbols[str(name).upper()] = (int(sym_id), int(dig))
        self.resolve_symbol()

    def resolve_symbol(self):
        if self.symbol_requested:
            return
        raw = self.args.currency.upper()
        candidates = [raw, raw.replace("/", ""), raw.replace("-", "")]
        found = None
        for cand in candidates:
            if cand in self.symbols:
                found = cand
                break
        if found is None:
            for name in self.symbols:
                if name.upper() == raw:
                    found = name
                    break
        if found is None:
            return
        self.resolved_name = found
        self.symbol_info = self.symbols[found]
        print(
            f"[SYMBOL] {self.resolved_name} -> FIX id {self.symbol_info[0]}, "
            f"digits {self.symbol_info[1]}"
        )
        self.request_market_data()

    def request_market_data(self):
        if self.symbol_requested or self.symbol_info is None:
            return
        self.symbol_requested = True
        req = SpotMarketDataRequest(self.quote_cfg)
        req.MDReqID = "md-1"
        req.SubscriptionRequestType = 1
        req.MarketDepth = 1
        req.MDUpdateType = 1
        req.NoMDEntryTypes = 2
        req.NoRelatedSym = 1
        req.Symbol = self.symbol_info[0]
        print(f"[PRICE] Requesting market data for {self.resolved_name}")
        self.quote_client.send(req).addErrback(self.on_send_error)

    def parse_market_data(self, message):
        types = message.getFieldValue(269)
        prices = message.getFieldValue(270)
        if types is None or prices is None:
            return
        if not isinstance(types, list):
            types = [types]
            prices = [prices]
        for entry_type, price in zip(types, prices):
            value = float(price)
            if int(entry_type) == 0:
                self.bid = value
            elif int(entry_type) == 1:
                self.ask = value
        if self.bid is None or self.ask is None:
            return
        digits = self.symbol_info[1]
        print(
            f"[PRICE] {self.resolved_name} bid={self.bid:.{digits}f} "
            f"ask={self.ask:.{digits}f}"
        )
        if self.args.price_only:
            self.finish(0)
            return
        if self.trade_logged and not self.order_placed:
            self.place_order()

    def place_order(self):
        self.order_placed = True
        side = self.args.side.lower()
        self.validate_sl_tp()
        units = lots_to_units(self.resolved_name, self.args.amount)
        if units <= 0:
            print(f"[ERROR] amount {self.args.amount} {self.resolved_name} -> 0 units "
                  f"(below the symbol's 1-lot size). Use a larger --amount.")
            self.finish(1)
            return
        self.units = units
        self.cl_ord_id = f"ord-{int(time.time() * 1000)}-{uuid.uuid4().hex[:6]}"
        side_value = 1 if side == "buy" else 2
        order = NewOrderSingle(self.trade_cfg)
        order.ClOrdID = self.cl_ord_id
        order.Symbol = self.symbol_info[0]
        order.Side = side_value
        order.OrderQty = units
        order.OrdType = 1
        order.TransactTime = datetime.datetime.now()
        order.Designation = "ctrader-fix"
        print(
            f"[ORDER] Market {side.upper()} {self.args.amount} {self.resolved_name} "
            f"({units} units), clOrdID={self.cl_ord_id}"
        )
        self.trade_client.send(order).addErrback(self.on_send_error)

    def validate_sl_tp(self):
        side = self.args.side.lower()
        entry = self.ask if side == "buy" else self.bid
        if self.args.sl is not None:
            sl = float(self.args.sl)
            bad_buy = side == "buy" and sl >= entry
            bad_sell = side == "sell" and sl <= entry
            if bad_buy or bad_sell:
                ref = "ask" if side == "buy" else "bid"
                print(f"[ERROR] Stop loss {sl} is on the wrong side of current {ref} {entry}")
                self.finish(1)
                return
        if self.args.tp is not None:
            tp = float(self.args.tp)
            bad_buy = side == "buy" and tp <= entry
            bad_sell = side == "sell" and tp >= entry
            if bad_buy or bad_sell:
                ref = "ask" if side == "buy" else "bid"
                print(f"[ERROR] Take profit {tp} is on the wrong side of current {ref} {entry}")
                self.finish(1)
                return

    def parse_execution_report(self, message):
        client_id = message.getFieldValue(11)
        exec_type = message.getFieldValue(150)
        is_market = client_id == self.cl_ord_id
        is_sl = client_id == self.sl_cl_ord_id
        is_tp = client_id == self.tp_cl_ord_id
        if not (is_market or is_sl or is_tp):
            return
        order_id = message.getFieldValue(37)
        status = message.getFieldValue(39)
        reject_reason = message.getFieldValue(58)

        if is_sl:
            self.sl_acked = exec_type != "8"
            tag = "SL"
        elif is_tp:
            self.tp_acked = exec_type != "8"
            tag = "TP"
        else:
            tag = None
        if tag is not None:
            if exec_type == "8":
                print(f"[ERROR] {tag} order rejected (clOrdID={client_id}): {reject_reason}")
                self.finish(1)
                return
            if exec_type in ("0", "I"):
                print(f"[{tag}] {tag} order ack order_id={order_id} status={status}")
            elif exec_type == "F":
                print(f"[{tag}] {tag} order filled order_id={order_id}")
            else:
                print(f"[{tag}] {tag} order status order_id={order_id} status={status}")

        if is_market:
            position_id = message.getFieldValue(721)
            if position_id is not None and self.position_id is None:
                self.position_id = position_id
                print(f"[ORDER] Ack order_id={order_id} position_id={position_id} status={status}")
            if exec_type == "8":
                print(f"[ERROR] Order rejected: {reject_reason}")
                self.finish(1)
                return
            if exec_type == "F":
                avg = message.getFieldValue(6)
                print(f"[FILL] position_id={position_id or self.position_id} avg_price={avg}")
                if not self.filled:
                    self.filled = True
                    self.send_sl_tp()
        elif is_sl or is_tp:
            if exec_type != "8":
                self.maybe_finish()

    def send_sl_tp(self):
        if self.position_id is None or not self.filled:
            return
        opposite = 2 if self.args.side.lower() == "buy" else 1
        if self.args.sl is not None and not self.sl_sent:
            self.sl_sent = True
            self.sl_cl_ord_id = f"sl-{int(time.time() * 1000)}-{uuid.uuid4().hex[:6]}"
            req = NewOrderSingle(self.trade_cfg)
            req.ClOrdID = self.sl_cl_ord_id
            req.Symbol = self.symbol_info[0]
            req.Side = opposite
            req.OrderQty = self.units
            req.OrdType = 3
            req.StopPx = float(self.args.sl)
            req.TransactTime = datetime.datetime.now()
            req.PosMaintRptID = self.position_id
            req.Designation = "ctrader-fix-sl"
            print(f"[SL] Attaching stop {self.args.sl} to position {self.position_id}")
            self.trade_client.send(req).addErrback(self.on_send_error)
        if self.args.tp is not None and not self.tp_sent:
            self.tp_sent = True
            self.tp_cl_ord_id = f"tp-{int(time.time() * 1000)}-{uuid.uuid4().hex[:6]}"
            req = NewOrderSingle(self.trade_cfg)
            req.ClOrdID = self.tp_cl_ord_id
            req.Symbol = self.symbol_info[0]
            req.Side = opposite
            req.OrderQty = self.units
            req.OrdType = 2
            req.Price = float(self.args.tp)
            req.TransactTime = datetime.datetime.now()
            req.PosMaintRptID = self.position_id
            req.Designation = "ctrader-fix-tp"
            print(f"[TP] Attaching take profit {self.args.tp} to position {self.position_id}")
            self.trade_client.send(req).addErrback(self.on_send_error)
        self.maybe_finish()

    def maybe_finish(self):
        if not self.filled or self._finished:
            return
        need_sl = self.args.sl is not None
        need_tp = self.args.tp is not None
        if not need_sl and not need_tp:
            self.finish(0)
            return
        if self.sl_sent and need_sl and not self.sl_acked:
            return
        if self.tp_sent and need_tp and not self.tp_acked:
            return
        self.finish(0)

    def on_timeout(self):
        if self._finished:
            return
        if self.resolved_name is None:
            if self.symbols:
                close = sorted(self.symbols.keys(), key=lambda n: n != self.args.currency.upper())
                print(
                    f"[TIMEOUT] Symbol '{self.args.currency}' not found in the security list. "
                    f"Received {len(self.symbols)} symbols. Closest matches: {close[:5]}"
                )
            else:
                print("[TIMEOUT] Security list was never received.")
        elif self.bid is None or self.ask is None:
            print("[TIMEOUT] No market data received for the resolved symbol.")
        else:
            print("[TIMEOUT] Operation did not complete in time")
        self.finish(1)

    def finish(self, code):
        if self._finished:
            return
        self._finished = True
        self.exit_code = code
        if self.trade_client is not None:
            try:
                self.trade_client.stopService()
            except Exception:
                pass
        if self.quote_client is not None:
            try:
                self.quote_client.stopService()
            except Exception:
                pass
        reactor.stop()


def build_parser():
    parser = argparse.ArgumentParser(
        description="Pepperstone cTrader FIX market buy/sell using ctrader-fix"
    )
    parser.add_argument("--side", choices=["buy", "sell"], help="buy or sell")
    parser.add_argument("--amount", type=float, help="trade size in lots, e.g. 0.01")
    parser.add_argument("--currency", required=True, help="symbol, e.g. EURUSD")
    parser.add_argument("--sl", type=float, help="absolute stop loss price")
    parser.add_argument("--tp", type=float, help="absolute take profit price")
    parser.add_argument("--price-only", action="store_true", help="only print bid/ask and exit")
    parser.add_argument("--mode", choices=["live", "demo"], default="live",
                        help="account mode (default live)")
    parser.add_argument("--quote-config", default=None,
                        help="quote session JSON config (default: config-quote.json or demo-config-quote.json by --mode)")
    parser.add_argument("--trade-config", default=None,
                        help="trade session JSON config (default: config-trade.json or demo-config-trade.json by --mode)")
    parser.add_argument("--host", help="FIX host (default from trade config)")
    parser.add_argument("--quote-port", type=int, default=QUOTE_PORT, help=f"quote port (default {QUOTE_PORT})")
    parser.add_argument("--trade-port", type=int, default=TRADE_PORT, help=f"trade port (default {TRADE_PORT})")
    parser.add_argument("--ssl", action="store_true", help="use SSL (broken in ctrader-fix, prefer plain ports)")
    parser.add_argument("--timeout", type=int, default=30, help="overall timeout in seconds")
    return parser


def main():
    parser = build_parser()
    args = parser.parse_args()
    if not args.price_only and not args.side:
        parser.error("--side is required unless --price-only is used")
    if not args.price_only and not args.amount:
        parser.error("--amount is required unless --price-only is used")

    mode = args.mode.lower()
    quote_path = args.quote_config or ("demo-config-quote.json" if mode == "demo" else "config-quote.json")
    trade_path = args.trade_config or ("demo-config-trade.json" if mode == "demo" else "config-trade.json")
    print(f"[MODE] {mode.upper()} trading")
    print(f"[MODE] quote config: {quote_path}")
    print(f"[MODE] trade config: {trade_path}")

    quote_cfg = load_config(quote_path)
    trade_cfg = load_config(trade_path)
    host = args.host or trade_cfg["Host"]

    bot = Bot(args, quote_cfg, trade_cfg)

    quote_client = Client(host, args.quote_port, ssl=args.ssl)
    trade_client = Client(host, args.trade_port, ssl=args.ssl)
    bot.quote_client = quote_client
    bot.trade_client = trade_client

    quote_client.setConnectedCallback(bot.on_connected)
    quote_client.setMessageReceivedCallback(bot.on_message)
    quote_client.setDisconnectedCallback(bot.on_disconnected)
    trade_client.setConnectedCallback(bot.on_connected)
    trade_client.setMessageReceivedCallback(bot.on_message)
    trade_client.setDisconnectedCallback(bot.on_disconnected)

    quote_client.startService()
    trade_client.startService()
    reactor.callLater(args.timeout, bot.on_timeout)
    reactor.run()
    sys.exit(bot.exit_code)


if __name__ == "__main__":
    main()