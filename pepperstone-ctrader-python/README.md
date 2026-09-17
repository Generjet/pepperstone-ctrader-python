# Pepperstone cTrader Market Order Scripts (FIX API)

Place market **buy** / **sell** orders on your Pepperstone cTrader account with optional
stop-loss and take-profit, using Python and the cTrader FIX API.

Three equivalent scripts are provided, one per library in `requirements.txt`:

| Script | Library | Status |
|---|---|---|
| `trade_ejtrader_ct.py` | `ejtraderCT` | **Recommended** - maintained, high-level |
| `trade_sinan_ct.py` | `SinanProjectCT` | Unmaintained / abandoned fork of the above (same API) |
| `trade_ctrader_fix.py` | `ctrader-fix` | Official Spotware package, low-level, asynchronous |

---

## 1. Requirements

- Python 3.7+ (tested on 3.13)
- A Pepperstone cTrader account with the **FIX API enabled** (a numeric FIX password is required, set it in cTrader → Settings → FIX API)
- The libraries from `requirements.txt`:

```bash
pip install -r requirements.txt
```

---

## 2. Configuration

Two JSON config files hold your FIX credentials. The `SenderCompID` looks like
`live.pepperstone.1057272` (environment.broker.login); the `Password` is the numeric
FIX API password, not your login password.

- `config-quote.json` - quote/price session (`SenderSubID: QUOTE`)
- `config-trade.json` - trade/order session (`SenderSubID: TRADE`)

The scripts derive `Username` and `TargetSubID` automatically when missing.

> **Security:** these files contain live credentials in plaintext. Do not commit them.
> Consider using environment variables or a secrets manager, and rotate the FIX API
> password if these files are ever shared.

---

## 3. Usage

All three scripts follow the same CLI. `--amount` is in **lots** (e.g. `0.01`).

### Get the current price only

```bash
python trade_ejtrader_ct.py --price-only --currency EURUSD
```

### Market BUY with stop-loss and take-profit

```bash
python trade_ejtrader_ct.py \
  --side buy \
  --amount 0.01 \
  --currency EURUSD \
  --tp 1.1050 \
  --sl 1.0950
```

### Market SELL with stop-loss and take-profit

```bash
python trade_ejtrader_ct.py \
  --side sell \
  --amount 0.01 \
  --currency GBPUSD \
  --tp 1.2100 \
  --sl 1.2200
```

### Same calls for the other libraries

```bash
python trade_sinan_ct.py      --side buy --amount 0.01 --currency EURUSD --tp 1.1050 --sl 1.0950
python trade_ctrader_fix.py   --side sell --amount 0.01 --currency EURUSD --tp 1.0950 --sl 1.1050
```

### Common arguments

| Argument | Description | Default |
|---|---|---|
| `--side` | `buy` or `sell` | required (unless `--price-only`) |
| `--amount` | trade size in lots, e.g. `0.01` | required (unless `--price-only`) |
| `--currency` | symbol, e.g. `EURUSD` | required |
| `--tp` | absolute take-profit price | none |
| `--sl` | absolute stop-loss price | none |
| `--price-only` | print bid/ask and exit, no order | off |

SL/TP are validated against the current price. For a **buy**, SL must be below and TP
above the current ask; the opposite for a **sell**.

### Library-specific options

`trade_ejtrader_ct.py` / `trade_sinan_ct.py`:

| Argument | Description | Default |
|---|---|---|
| `--config` | FIX JSON config path | `config-quote.json` |
| `--base-currency` | account base currency | `USD` |
| `--client-id` | label used on orders | `1` |
| `--timeout` | seconds to wait for a quote | `20.0` |

`trade_ctrader_fix.py`:

| Argument | Description | Default |
|---|---|---|
| `--quote-config` | quote session JSON config | `config-quote.json` |
| `--trade-config` | trade session JSON config | `config-trade.json` |
| `--host` | FIX host | from trade config |
| `--quote-port` | quote session port | `5201` |
| `--trade-port` | trade session port | `5202` |
| `--ssl` | enable SSL (broken in this library) | off |
| `--timeout` | overall operation timeout | `30` |

---

## 4. Network / ports

cTrader FIX uses these ports on the host shown in cTrader → Settings → FIX API:

| Connection | Plain text | SSL |
|---|---|---|
| QUOTE (prices) | `5201` | `5211` |
| TRADE (orders) | `5202` | `5212` |

- `ejtraderCT` / `SinanProjectCT` always use the **plain** ports `5201`/`5202`
  internally; the `Port` field in the config is ignored.
- `ctrader-fix` defaults to plain `5201`/`5202` because **SSL is known to be broken**
  in version 0.1.1. If you pass `--ssl`, also switch ports to `5211`/`5212`.

---

## 5. How SL/TP work (read this before trading)

The cTrader FIX dictionary does **not** allow stop-loss / take-profit inside the
`NewOrderSingle` (market order) message. All three scripts attach them the official way:

1. Place the market order to open the position.
2. Read the `PosMaintRptID` (position ID, tag 721) from the execution report.
3. Attach a **Stop order** at the SL price and a **Limit order** at the TP price,
   both tied to the position via tag 721.

**Known limitation** (documented by the ejtraderCT authors): when *both* SL and TP are
set and *one* triggers, the other order can remain live and open a new position in the
opposite direction. If this matters, use only SL **or** only TP at a time, or cancel the
counterpart order once one has triggered.

---

## 6. Lot size conversion

Volume is passed in lots and converted to FIX units internally:

| Instrument | Example | Units per lot |
|---|---|---|
| Forex pairs | EURUSD, GBPJPY | `100 000` |
| Gold / Silver | XAUUSD, XAGUSD | `100` |
| Crypto | BTCUSD, ETHUSD | `1` |

---

## 7. Troubleshooting

| Symptom | Likely cause / fix |
|---|---|
| `Not logged in` | Wrong host, wrong numeric FIX API password, or FIX API not enabled in cTrader |
| Connection timeout on the quote session | Wrong port / host; use the exact values from cTrader → Settings → FIX API |
| `[TIMEOUT] Operation did not complete` | Markets closed for the symbol, or symbol name is not listed by your broker |
| Symbol not found | Some brokers list symbols like `EURUSD` vs `EUR/USD`; the script tries both spellings. If it still fails, check the exact name in cTrader |

---

## 8. Example: use in another script

The high-level libraries are easy to call from your own strategy code:

```python
from ejtraderCT import Ctrader
import time

api = Ctrader("live-us-eqx-01.p.c-trader.com", "live.pepperstone.1057272", "YOUR_FIX_PASSWORD")
time.sleep(2)

api.subscribe("EURUSD")
quote = api.quote("EURUSD")          # {'bid': ..., 'ask': ...}

ticket = api.buy("EURUSD", 0.01, 1.0950, 1.1050)   # buy 0.01 lots with SL/TP
api.logout()
```