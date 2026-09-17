# FX Trading Bot for Pepperstone (cTrader)

A cross-platform desktop trading bot app for Forex. The Go backend runs as a
small native binary (Linux / Windows / macOS) and opens a fast chart UI in your
default browser. Placing orders is delegated to the Pepperstone cTrader Python
scripts in [`pepperstone-ctrader-python/`](pepperstone-ctrader-python/).

## What it does

1. Fetches Forex candlestick data from **Yahoo Finance** (e.g. `USDJPY`, `EURUSD`)
2. Calculates **Stochastic Oscillator**, **RSI** and **HMA** (Hull Moving Average)
3. Shows an interactive **candlestick + indicator chart** with buy/sell markers
4. TOP page: currency, interval, lots, strategy selector, mode (demo/live), start/stop
5. TRADE page: candlestick chart, RSI and Stochastic subplots, trades table
6. **Sells only if profitable** — a SELL is executed only when the live broker
   bid is higher than the buy price. Every trade is saved with `buyDate` /
   `sellDate` and kept in `data/trades.json`

### Strategies

| Strategy | Rule |
|---|---|
| RSI only | Buy when RSI crosses up through the floor; sell when it crosses down through the ceiling |
| Stochastic only | Buy on bullish %K/%D crossover; sell on bearish %K/%D crossover |
| RSI + Stochastic | Bullish %K/%D crossover confirmed by RSI below floor → buy; bearish crossover confirmed by RSI above ceiling → sell |
| HMA trend | Buy when price crosses above HMA; sell when price crosses below HMA |

## How orders are placed

The bot never talks to the broker directly. It shells out to one of your
existing Pepperstone scripts (selected in the UI, default `trade_ejtrader_ct.py`):

- `trade_ejtrader_ct.py` (recommended, uses `ejtraderCT`)
- `trade_sinan_ct.py` (uses `SinanProjectCT`)
- `trade_ctrader_fix.py` (low-level, uses `ctrader-fix`)

The scripts connect with the FIX credentials from `config-quote.json` /
`config-trade.json` (live) or `demo-config-*.json` (demo). **Never commit these
files** — they contain your plaintext FIX password. Default mode in the UI is
`demo` for safety.

## Build

Pure Go, no CGO. Build native binaries for Linux and Windows:

```bash
./build.sh
# outputs dist/fxbot-linux-amd64 and dist/fxbot-windows-amd64.exe
```

## Run

From the repo root (the app looks for `pepperstone-ctrader-python/` next to the
binary by default):

```bash
./dist/fxbot-linux-amd64            # Linux
dist/fxbot-windows-amd64.exe        # Windows
```

Options:

| Flag | Default | Meaning |
|---|---|---|
| `-addr` | `127.0.0.1:0` | Listen address (free port by default) |
| `-data` | `data` | Trades ledger + CSV/HTML exports |
| `-pepperstone` | `pepperstone-ctrader-python` | Folder with the python scripts + venv |
| `-open-browser` | `true` | Open the UI in the default browser |

The app prints the URL it is serving on — open it in any browser.

### Windows prerequisites

The Go binary itself has no dependencies, but it invokes Python. On Windows you
need Python 3.7+ with the venv from `pepperstone-ctrader-python/` created
(`venv\Scripts\python.exe`). Run the bot from a directory that contains the
`venv` and the `*.json` config files.

## Saved data

- `data/trades.json` — persistent trade ledger (buyDate / sellDate / prices / profit)
- `data/<SYMBOL>_<INTERVAL>_<ts>.csv` — candles + indicators + signals
- `data/<SYMBOL>_<INTERVAL>_<ts>.html` — standalone Plotly report (hit **Export**)

## API

- `GET /api/meta` — available symbols, intervals, strategies, scripts
- `GET /api/data?symbol=USDJPY&interval=1h` — candles + indicators + signals
- `GET /api/status` — bot status and open position
- `GET /api/trades` — trade history
- `POST /api/start` / `POST /api/stop` — control the bot
- `POST /api/export` — write CSV + HTML report to `data/`
- `GET /api/logs`, `GET /api/stream` (SSE) — log tail / live updates