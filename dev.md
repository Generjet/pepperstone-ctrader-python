- you will create automatic trading bot app for Binance. Develop my app and create build script, so that it can build it for linux, windows native app.

# basically here is what you need to do:
1. Fetch data from Binance server
2. Calculate: Stochastic Oscillator, RSI
3. Plot candlestick chart with Indicators

# You create GUI with input parameters(with default values) and chart:
4. TOP Page has inputs: currency(ETHUSDT by default), interval(1h by default),
"start" button, select strategy from selectbox (select strategies you created). Buy/Sell action must be performed
only for selected strategies. For example if selected only RSI, then trade by RSI only, and if selected RSI with Stochastic then trade by RSI and Stochastic.
5. TRADE Page with candlestick charts, indicator charts, trades table.

# Basic imagination about trade
6. Sell only if profitable, in other words sell if current price is higher than buyPrice.
To recognize, whether trade is profitable or not it may need to save every trade. Therefore, there may need table "trade":
- buyDate
- sellDate

