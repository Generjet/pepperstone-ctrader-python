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

`patch_order_methods()` fixes the SinanProjectCT buy() None-stoploss crash and
makes broker order rejections (e.g. insufficient funds) abort the script with a
logged error instead of hanging the process forever.
"""

import datetime
import logging
import os
import sys
import time

_LOG_DIR = os.path.join(os.path.dirname(os.path.abspath(__file__)), "log")

try:
    _DEFAULT_WAIT_TIMEOUT = int(os.environ.get("TRADE_WAIT_TIMEOUT", "15"))
except ValueError:
    _DEFAULT_WAIT_TIMEOUT = 15


class OrderRejected(BaseException):
    """Raised when a submitted order is rejected or times out waiting for a fill.

    Deliberately subclasses BaseException, NOT Exception: the SinanProjectCT /
    ejtraderCT fill-wait loop swallows `except Exception` (while True/continue),
    so a normal Exception would hang forever. BaseException bypasses it.

    `clid` is the library order id (ClOrdId) so the caller can cancel a
    still-pending order on timeout; `timeout` distinguishes a dead-broker fill
    timeout from an actual broker rejection (e.g. insufficient funds).
    """

    def __init__(self, message, clid=None, timeout=False):
        super().__init__(message)
        self.clid = clid
        self.timeout = timeout


class _RejectDict(dict):
    """origin_to_pos_id stand-in that raises OrderRejected for rejected orders.

    The library's trade() wait loop reads `self.fix.origin_to_pos_id[v_ticket]`
    inside `try/except Exception/continue`; when the broker rejected that order
    the key never appears and the loop spins forever. dict.__getitem__-on-missing
    calls __missing__, and our version raises OrderRejected (a BaseException),
    which the loop's `except Exception` cannot catch - so trade() aborts.
    Also raises once a hard deadline passes with no fill and no rejection, so a
    broker that goes silent cannot hang the script under cron.
    """

    def __init__(self, *args, rejected=None, deadline=None, wait=0, **kwargs):
        super().__init__(*args, **kwargs)
        self._rejected = rejected if rejected is not None else {}
        self._deadline = deadline
        self._wait = wait

    def __missing__(self, key):
        if key in self._rejected:
            raise OrderRejected(self._rejected[key], clid=key)
        if self._deadline is not None and time.time() > self._deadline:
            raise OrderRejected(
                f"no fill and no rejection within {self._wait}s for order {key}",
                clid=key,
                timeout=True,
            )
        raise KeyError(key)


def _arm_reject(instance, timeout=_DEFAULT_WAIT_TIMEOUT):
    """Ensure fix.rejected exists and origin_to_pos_id raises on rejection/fill-wait timeout."""
    fix = getattr(instance, "fix", None)
    if fix is None:
        return
    rejected = getattr(fix, "rejected", None)
    if rejected is None:
        rejected = {}
        fix.rejected = rejected
    if not isinstance(fix.origin_to_pos_id, _RejectDict):
        fix.origin_to_pos_id = _RejectDict(fix.origin_to_pos_id, rejected=rejected)
    fix.origin_to_pos_id._deadline = time.time() + timeout
    fix.origin_to_pos_id._wait = timeout


def _wait_fill_or_reject(instance, ticket, timeout=_DEFAULT_WAIT_TIMEOUT):
    """Wait for the fire-and-forget order; raise on rejection or timeout."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        rejected = getattr(instance.fix, "rejected", {})
        if ticket in rejected:
            raise OrderRejected(rejected[ticket], clid=ticket)
        if ticket in getattr(instance.fix, "origin_to_pos_id", {}):
            return
        time.sleep(0.1)
    raise OrderRejected(
        f"no fill and no rejection within {timeout}s for order {ticket}",
        clid=ticket,
        timeout=True,
    )


def _cancel_pending(instance, clid):
    """Send a FIX OrderCancelRequest (35=F) for a still-pending order."""
    fix = getattr(instance, "fix", None)
    cancel = getattr(fix, "cancel_order", None)
    if cancel is None:
        logging.warning("[ORDER] Cannot cancel %s: cancel_order() unavailable", clid)
        return
    try:
        cancel(clid)
        logging.warning("[ORDER] Cancel requested for pending order %s", clid)
    except Exception as exc:
        logging.error("[ORDER] Cancel failed for %s: %s", clid, exc)


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


def _patch_reject_handling(module):
    """Record broker order rejections so scripts can fail fast instead of hanging.

    SinanProjectCT/ejtraderCT reject a bad order (e.g. insufficient funds) with a
    FIX Reject / BusinessMessageReject (MsgType 3/9/j, tag 58 = e.g.
    "NOT_ENOUGH_MONEY:Not enough funds", tag 379 = the rejected order's ClOrdId).
    The library only logs this and never feeds the fill-wait loop, so the script
    hangs forever. This wrapper records the rejection in `fix.rejected[cl_ord_id]`
    and logs it. The dispatch dict is mutated (the library calls
    `FIX.message_dispatch[msg_type](self, msg)` at runtime), so patching the
    attribute alone would not take effect.
    """
    fix_cls = getattr(module, "FIX", None)
    if fix_cls is None:
        return
    if getattr(fix_cls, "_reject_patched", False):
        return
    fix_cls._reject_patched = True
    dispatch = getattr(fix_cls, "message_dispatch", None)
    if not isinstance(dispatch, dict):
        return

    def _record_reject(self, clid, reason):
        if clid:
            rejected = getattr(self, "rejected", None)
            if rejected is None:
                rejected = {}
                self.rejected = rejected
            rejected[clid] = reason
        logging.warning("[ORDER REJECTED] %s: %s", clid or "?", reason)

    original_exec_report = dispatch["8"]

    def _exec_report(self, msg):
        exec_type = msg[150]
        ord_status = msg[39]
        if exec_type == "8" or ord_status == "8":
            clid = msg[11] or None
            reason = (
                msg[58]
                or (("reject reason " + msg[103]) if msg[103] else None)
                or "rejected"
            )
            _record_reject(self, clid, reason)
        return original_exec_report(self, msg)

    def _reject_wrapper(self, msg):
        text = msg[58] or ""
        reason = text.split(":")[-1].strip() or "rejected"
        clid = str(msg[379] or msg[11] or "")
        if clid.isdigit() and len(clid) >= 8:
            _record_reject(self, clid, reason)
        else:
            logging.debug("[REJECT] session-level (no order id): %s", text)
        try:
            return reject_func(self, msg)
        except Exception:
            return None

    dispatch["8"] = _exec_report
    reject_func = dispatch.get("j") or dispatch.get("9") or dispatch.get("3")
    if reject_func is not None:
        for key in ("3", "9", "j"):
            dispatch[key] = _reject_wrapper


def patch_order_methods(module):
    """Fix broken buy()/sell() SL/TP argument mapping and make rejections fail fast.

    SinanProjectCT.api.ctrader.buy() passes None in the stoploss slot of
    trade(), so any order with a stop loss crashes with
    "TypeError: float() argument ... not 'NoneType'" and drops the SL entirely.
    These wrappers call trade() with the correct positional mapping, coerce
    unset SL/TP to 0 so the library's float() guards work, and surface broker
    rejections as OrderRejected instead of hanging. A hard fill-wait deadline
    (TRADE_WAIT_TIMEOUT, default 15s) is enforced: if the broker neither fills
    nor rejects in time, the pending order is cancelled with 35=F before the
    script raises OrderRejected. Real rejections (e.g. insufficient funds) are
    never cancelled - they are already dead at the broker.
    """
    cls = getattr(module, "Ctrader", None)
    if cls is None:
        return
    _patch_reject_handling(module)

    def _fixed_buy(self, symbol, volume, stoploss=None, takeprofit=None, price=0):
        _arm_reject(self)
        try:
            ticket = self.trade(
                symbol, "OPEN", 0, "buy",
                volume, stoploss or 0, takeprofit or 0, price, None, None,
            )
            if not stoploss and not takeprofit:
                _wait_fill_or_reject(self, ticket)
        except OrderRejected as exc:
            if exc.timeout and exc.clid and exc.clid not in getattr(
                self.fix, "rejected", {}
            ):
                _cancel_pending(self, exc.clid)
            raise
        return ticket

    def _fixed_sell(self, symbol, volume, stoploss=None, takeprofit=None, price=0):
        _arm_reject(self)
        try:
            ticket = self.trade(
                symbol, "OPEN", 1, "sell",
                volume, stoploss or 0, takeprofit or 0, price, None, None,
            )
            if not stoploss and not takeprofit:
                _wait_fill_or_reject(self, ticket)
        except OrderRejected as exc:
            if exc.timeout and exc.clid and exc.clid not in getattr(
                self.fix, "rejected", {}
            ):
                _cancel_pending(self, exc.clid)
            raise
        return ticket

    if not getattr(cls.buy, "_order_patched", False):
        _fixed_buy._order_patched = True
        cls.buy = _fixed_buy
    if not getattr(cls.sell, "_order_patched", False):
        _fixed_sell._order_patched = True
        cls.sell = _fixed_sell