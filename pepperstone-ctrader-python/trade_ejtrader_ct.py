import argparse
import json
import os
import sys
import time

from ejtraderCT import Ctrader
from ejtraderCT.api import ctrader as _ejtrader_lib

import trade_log

trade_log.patch_position_callback(_ejtrader_lib)
trade_log.patch_order_methods(_ejtrader_lib)
trade_log.configure("trade_ejtrader_ct")


def load_config(path):
    with open(path) as handle:
        return json.load(handle)


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


def build_parser():
    parser = argparse.ArgumentParser(
        description="Pepperstone cTrader FIX market buy/sell using ejtraderCT"
    )
    parser.add_argument("--side", choices=["buy", "sell"], help="buy or sell")
    parser.add_argument("--amount", type=float, help="trade size in lots, e.g. 0.01")
    parser.add_argument("--currency", required=True, help="symbol, e.g. EURUSD")
    parser.add_argument("--sl", type=float, help="absolute stop loss price")
    parser.add_argument("--tp", type=float, help="absolute take profit price")
    parser.add_argument("--price-only", action="store_true", help="only print bid/ask and exit")
    parser.add_argument("--mode", choices=["live", "demo"], default="live",
                        help="account mode (default live)")
    parser.add_argument("--config", default=None,
                        help="FIX JSON config (default: config-trade.json or demo-config-trade.json by --mode)")
    parser.add_argument("--base-currency", default="USD", help="account base currency")
    parser.add_argument("--client-id", help="label/client id used on orders")
    parser.add_argument("--timeout", type=float, default=20.0, help="quote wait timeout seconds")
    return parser


def wait_for_quote(api, symbol, timeout):
    deadline = time.time() + timeout
    while time.time() < deadline:
        quote = api.quote(symbol)
        if isinstance(quote, dict) and "bid" in quote:
            return quote
        time.sleep(0.3)
    return None


def validate_sl_tp(side, entry, sl, tp, entry_name):
    if sl is not None:
        sl = float(sl)
        bad_buy = side == "buy" and sl >= entry
        bad_sell = side == "sell" and sl <= entry
        if bad_buy or bad_sell:
            print(f"[ERROR] Stop loss {sl} is on the wrong side of current {entry_name} {entry}")
            os._exit(1)
    if tp is not None:
        tp = float(tp)
        bad_buy = side == "buy" and tp <= entry
        bad_sell = side == "sell" and tp >= entry
        if bad_buy or bad_sell:
            print(f"[ERROR] Take profit {tp} is on the wrong side of current {entry_name} {entry}")
            os._exit(1)


def main():
    parser = build_parser()
    args = parser.parse_args()
    if not args.price_only and not args.side:
        parser.error("--side is required unless --price-only is used")
    if not args.price_only and not args.amount:
        parser.error("--amount is required unless --price-only is used")

    mode = args.mode.lower()
    config_path = args.config or ("demo-config-trade.json" if mode == "demo" else "config-trade.json")
    print(f"[MODE] {mode.upper()} trading")
    print(f"[MODE] config: {config_path}")

    cfg = load_config(config_path)
    client_id = args.client_id or 1

    print(f"[CONNECT] {cfg['SenderCompID']} @ {cfg['Host']}")
    api = Ctrader(
        server=cfg["Host"],
        account=cfg["SenderCompID"],
        password=cfg["Password"],
        currency=args.base_currency,
        client_id=client_id,
    )
    time.sleep(2)

    if not api.isconnected():
        print("[ERROR] Not logged in - check host, credentials and FIX API password")
        os._exit(1)
    print("[CONNECT] Quote and Trade sessions OK")

    symbol = args.currency.upper()
    api.subscribe(symbol)

    quote = wait_for_quote(api, symbol, args.timeout)
    if quote is None:
        print(f"[ERROR] No quote received for {symbol}")
        api.logout()
        os._exit(1)

    bid = float(quote["bid"])
    ask = float(quote["ask"])
    print(f"[PRICE] {symbol} bid={bid} ask={ask}")

    if args.price_only:
        api.logout()
        os._exit(0)

    side = args.side.lower()
    entry = ask if side == "buy" else bid
    entry_name = "ask" if side == "buy" else "bid"
    validate_sl_tp(side, entry, args.sl, args.tp, entry_name)

    units = lots_to_units(symbol, args.amount)
    if units <= 0:
        print(f"[ERROR] amount {args.amount} {symbol} -> 0 units "
              f"(below the symbol's 1-lot size). Use a larger --amount.")
        api.logout()
        os._exit(1)
    sl = float(args.sl) if args.sl is not None else 0
    tp = float(args.tp) if args.tp is not None else 0

    print(f"[ORDER] Market {side.upper()} {args.amount} {symbol} ({units} units)")
    try:
        if side == "buy":
            ticket = api.buy(symbol, args.amount, sl, tp)
        else:
            ticket = api.sell(symbol, args.amount, sl, tp)
    except trade_log.OrderRejected as exc:
        print(f"[ERROR] Order rejected: {exc}")
        api.logout()
        os._exit(1)
    print(f"[ORDER] Sent, ticket={ticket}")

    if sl or tp:
        print("[NOTE] SL and TP are attached as separate orders; if both are set and one triggers, "
              "the other may remain live (known cTrader FIX limitation).")
    api.logout()
    print("[DONE] Logged out")
    os._exit(0)


if __name__ == "__main__":
    main()