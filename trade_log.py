"""Shared helpers: file logging + patching broken ejtraderCT/SinanProjectCT behavior.

`configure()` tees stdout/stderr into a timestamped file under ./log so every
trade run leaves a durable log next to the code.

`patch_position_callback()` fixes the library crash that kills the quote reader
thread whenever the account holds an open position whose quote currency equals
the account base currency (e.g. XRPUSD on a USD account). The library reads
`position["convert"]` unconditionally, but that key is only written when a
conversion pair is needed, so the callback raises KeyError('convert'); the
exception is caught in the library's quote worker, which then stops reading
quotes entirely -> "No quote received for ...".
"""

import datetime
import os
import sys

_LOG_DIR = os.path.join(os.path.dirname(os.path.abspath(__file__)), "log")


class _Tee:
    def __init__(self, *streams):
        self._streams = streams

    def write(self, data):
        for stream in self._streams:
            stream.write(data)
            stream.flush()

    def flush(self):
        for stream in self._streams:
            try:
                stream.flush()
            except Exception:
                pass

    def isatty(self):
        return False

    def fileno(self):
        return self._streams[0].fileno()


def configure(script_name):
    """Create the log/ folder and tee stdout/stderr into a timestamped file."""
    os.makedirs(_LOG_DIR, exist_ok=True)
    stamp = datetime.datetime.now().strftime("%Y%m%d-%H%M%S")
    path = os.path.join(_LOG_DIR, f"{script_name}-{stamp}.log")
    file_handle = open(path, "a", encoding="utf-8")
    sys.stdout = _Tee(sys.__stdout__, file_handle)
    sys.stderr = _Tee(sys.__stderr__, file_handle)
    print(f"[LOG] Writing logs to {path}")
    return path


def patch_position_callback(module):
    """Make Ctrader.position_list_callback tolerate missing 'convert' keys.

    Must be called before Ctrader(<...>) is constructed, because the FIX session
    starts its reader threads inside the constructor and can crash immediately
    once market data for an already-open position arrives.
    """
    if not hasattr(module, "Ctrader"):
        return
    original = module.Ctrader.position_list_callback
    if getattr(original, "_convert_patched", False):
        return

    def safe(self, data, price_data, client_id):
        for position in data.values():
            position.setdefault("convert", None)
            position.setdefault("convert_dir", 0)
        return original(self, data, price_data, client_id)

    safe._convert_patched = True
    module.Ctrader.position_list_callback = safe


def patch_order_methods(module):
    """Fix broken buy()/sell() SL/TP argument mapping in third-party libs.

    SinanProjectCT.api.ctrader.buy() passes None in the stoploss slot of
    trade(), so any order with a stop loss crashes with
    "TypeError: float() argument ... not 'NoneType'" and drops the SL entirely.
    These wrappers call trade() with the correct positional mapping and coerce
    unset SL/TP to 0 so the library's float() guards work.
    """
    cls = getattr(module, "Ctrader", None)
    if cls is None:
        return

    def _fixed_buy(self, symbol, volume, stoploss=None, takeprofit=None, price=0):
        return self.trade(
            symbol, "OPEN", 0, "buy",
            volume, stoploss or 0, takeprofit or 0, price, None, None,
        )

    def _fixed_sell(self, symbol, volume, stoploss=None, takeprofit=None, price=0):
        return self.trade(
            symbol, "OPEN", 1, "sell",
            volume, stoploss or 0, takeprofit or 0, price, None, None,
        )

    if not getattr(cls.buy, "_order_patched", False):
        _fixed_buy._order_patched = True
        cls.buy = _fixed_buy
    if not getattr(cls.sell, "_order_patched", False):
        _fixed_sell._order_patched = True
        cls.sell = _fixed_sell