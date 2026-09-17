#!/usr/bin/env python3
"""Print open positions and open orders as easy-to-read tables.

Connects over FIX, lets the account's position/order snapshots and live quotes
arrive, then renders the open trades and pending orders with `tabulate`.
Supports --mode live|demo like the trade scripts.
"""

import argparse
import json
import os
import sys
import time

from tabulate import tabulate

from SinanProjectCT import Ctrader
from SinanProjectCT.api import ctrader as _sinan_lib

import trade_log

trade_log.patch_position_callback(_sinan_lib)
trade_log.configure("show_orders")

_LOT_SCALE = {
    "BTCUSD": 1,
    "ETHUSD": 1,
    "XAUUSD": 100,
    "XAUEUR": 100,
    "XAGUSD": 5000,
    "XAGEUR": 5000,
    "ADAUSD": 100,
    "XRPUSD": 100,
    "DOGEUSD": 100,
    "SOLUSD": 100,
    "AVAXUSD": 100,
    "DOTUSD": 100,
    "LINKUSD": 100,
    "MATICUSD": 100,
    "SHIBUSD": 100,
    "LTCUSD": 100,
    "BCHUSD": 100,
    "UNIUSD": 100,
    "AAVEUSD": 100,
    "ATOMUSD": 100,
    "FILUSD": 100,
}
_DEFAULT_SCALE = 100_000


def load_config(path):
    with open(path) as handle:
        return json.load(handle)


def units_to_lots(symbol, units):
    """Inverse of lots_to_units: convert FIX units back to lots for display."""
    scale = _LOT_SCALE.get(str(symbol).upper(), _DEFAULT_SCALE)
    return float(units) / scale


def fmt_lots(lots):
    return f"{lots:.4f}".rstrip("0").rstrip(".")


def order_type_label(order_type):
    try:
        ot = int(order_type)
    except (TypeError, ValueError):
        return str(order_type)
    return {1: "Market", 2: "Limit", 3: "Stop", 4: "StopLimit"}.get(ot, str(ot))


def _as_float(value):
    try:
        return float(str(value).replace(",", ""))
    except (TypeError, ValueError):
        return None


def ensure_subscribed(api, wanted, seen):
    """Subscribe each symbol at most once; skips ones the library already asked for."""
    requested = getattr(api.fix, "spot_request_list", set())
    newly = [s for s in wanted if s not in seen and s not in requested]
    for symbol in newly:
        try:
            api.subscribe(symbol)
        except Exception:
            pass
        seen.add(symbol)
    return newly


def build_parser():
    parser = argparse.ArgumentParser(
        description="Pepperstone cTrader FIX - print open positions and open orders"
    )
    parser.add_argument("--mode", choices=["live", "demo"], default="live",
                        help="account mode (default live)")
    parser.add_argument("--config", default=None,
                        help="FIX JSON config (default: config-trade.json or demo-config-trade.json by --mode)")
    parser.add_argument("--base-currency", default="USD", help="account base currency")
    parser.add_argument("--client-id", help="label/client id")
    parser.add_argument("--wait", type=float, default=6.0,
                        help="seconds to collect position/order snapshots and quotes before printing")
    parser.add_argument("--format", default="grid",
                        choices=["grid", "simple", "plain", "rounded_outline", "psql"],
                        help="tabulate table format (default grid)")
    return parser


def main():
    parser = build_parser()
    args = parser.parse_args()

    mode = args.mode.lower()
    config_path = args.config or ("demo-config-trade.json" if mode == "demo" else "config-trade.json")
    print(f"[MODE] {mode.upper()}")
    print(f"[MODE] config: {config_path}")

    cfg = load_config(config_path)
    print(f"[CONNECT] {cfg['SenderCompID']} @ {cfg['Host']}")
    api = Ctrader(
        server=cfg["Host"],
        account=cfg["SenderCompID"],
        password=cfg["Password"],
        currency=args.base_currency,
        client_id=args.client_id or 1,
    )
    time.sleep(2)

    if not api.isconnected():
        print("[ERROR] Not logged in - check host, credentials and FIX API password")
        os._exit(1)
    print("[CONNECT] Quote and Trade sessions OK")

    seen = set()
    deadline = time.time() + args.wait
    while time.time() < deadline:
        wanted = set()
        for row in api.positions():
            wanted.add(str(row.get("name", "")))
        for row in api.orders():
            wanted.add(str(row.get("name", "")))
        wanted.discard("")
        ensure_subscribed(api, wanted, seen)
        time.sleep(0.5)

    print()
    print("OPEN POSITIONS")
    position_rows = []
    for p in api.positions():
        position_rows.append([
            p.get("pos_id", ""),
            p.get("name", ""),
            p.get("side", ""),
            fmt_lots(units_to_lots(p.get("name", ""), p.get("amount", 0) or 0)),
            p.get("price", ""),
            p.get("actual_price", ""),
            p.get("diff", ""),
            p.get("pl", ""),
            p.get("gain", ""),
        ])
    if position_rows:
        print(tabulate(
            position_rows,
            headers=["Pos ID", "Symbol", "Side", "Qty (lots)",
                     "Open", "Now", "Diff", "P/L", "P/L (base)"],
            tablefmt=args.format,
        ))
    else:
        print("(no open positions)")

    total_pl = 0.0
    total_base = 0.0
    missing = 0
    for p in api.positions():
        pl = _as_float(p.get("pl"))
        gain = _as_float(p.get("gain"))
        if pl is not None:
            total_pl += pl
        else:
            missing += 1
        if gain is not None:
            total_base += gain
    print()
    print("TOTAL P/L (open positions)")
    print(tabulate(
        [[f"{total_pl:+.2f}", f"{total_base:+.2f} {args.base_currency.upper()}"]],
        headers=["P/L (quote)", "P/L (base)"],
        tablefmt=args.format,
        disable_numparse=True,
    ))
    if missing:
        print(f"[NOTE] {missing} position(s) without live quote - total misses their P/L")

    print()
    print("OPEN / PENDING ORDERS")
    types = {
        str(ord_id): row.get("type")
        for ord_id, row in getattr(api.fix, "order_list", {}).items()
    }
    order_rows = []
    for o in api.orders():
        order_rows.append([
            o.get("ord_id", ""),
            o.get("name", ""),
            o.get("side", ""),
            fmt_lots(units_to_lots(o.get("name", ""), o.get("amount", 0) or 0)),
            order_type_label(types.get(str(o.get("ord_id", "")))),
            o.get("price", ""),
            o.get("actual_price", ""),
            o.get("pos_id", ""),
            o.get("clid", ""),
        ])
    if order_rows:
        print(tabulate(
            order_rows,
            headers=["Order ID", "Symbol", "Side", "Qty (lots)",
                     "Type", "Trigger", "Now", "Position", "ClOrdId"],
            tablefmt=args.format,
        ))
    else:
        print("(no open orders)")

    print()
    api.logout()
    print("[DONE] Logged out")
    os._exit(0)


if __name__ == "__main__":
    try:
        main()
    except SystemExit:
        raise
    except BaseException as exc:
        import traceback
        traceback.print_exc()
        os._exit(1)