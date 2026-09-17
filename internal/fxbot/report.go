package fxbot

import (
	"bytes"
	"fmt"
	"html"
	"math"
	"os"
	"strings"
	"time"
)

// WriteReport renders a standalone Plotly HTML report of the candle rows with
// buy/sell markers and RSI / Stochastic / HMA indicator panes.
func WriteReport(path, symbol, interval string, rows []DataRow, p Params) error {
	var buf bytes.Buffer
	timeFmt := "2006-01-02 15:04:05"

	var timeStrs, opens, highs, lows, closes, hmas []string
	var rsiVals, stochKVals, stochDVals []string
	var buyTimes, buyPrices, sellTimes, sellPrices []string

	for _, r := range rows {
		if math.IsNaN(r.RSI) || math.IsNaN(r.StochK) || math.IsNaN(r.StochD) {
			continue
		}
		ts := fmt.Sprintf(`"%s"`, r.Time.Format(timeFmt))
		timeStrs = append(timeStrs, ts)
		opens = append(opens, f64(r.Open))
		highs = append(highs, f64(r.High))
		lows = append(lows, f64(r.Low))
		closes = append(closes, f64(r.Close))
		hmas = append(hmas, f64(r.HMA))
		rsiVals = append(rsiVals, f64(r.RSI))
		stochKVals = append(stochKVals, f64(r.StochK))
		stochDVals = append(stochDVals, f64(r.StochD))
		if r.Buy == 1 {
			buyTimes = append(buyTimes, ts)
			buyPrices = append(buyPrices, f64(r.Close))
		}
		if r.Sell == 1 {
			sellTimes = append(sellTimes, ts)
			sellPrices = append(sellPrices, f64(r.Close))
		}
	}

	title := fmt.Sprintf("%s (%s) — %s", symbol, interval, strategyLabel(p.StrategyID))
	buf.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>` + html.EscapeString(title) + `</title>
<script src="https://cdn.plot.ly/plotly-2.35.2.min.js"></script>
<style>
* { margin: 0; padding: 0; box-sizing: border-box; }
body { background: #1a1a2e; color: #eee; font-family: 'Segoe UI', Arial, sans-serif; padding: 20px; }
h1 { text-align: center; margin-bottom: 16px; font-size: 18px; color: #ccc; }
#chart { width: 100%; height: 100vh; }
</style>
</head>
<body>
<h1>` + html.EscapeString(title) + `</h1>
<div id="chart"></div>
<script>
var times=[` + strings.Join(timeStrs, ",") + `];
var opens=[` + strings.Join(opens, ",") + `]; var highs=[` + strings.Join(highs, ",") + `];
var lows=[` + strings.Join(lows, ",") + `]; var closes=[` + strings.Join(closes, ",") + `];
var hma=[` + strings.Join(hmas, ",") + `];
var rsi=[` + strings.Join(rsiVals, ",") + `]; var stochK=[` + strings.Join(stochKVals, ",") + `]; var stochD=[` + strings.Join(stochDVals, ",") + `];
var buyTimes=[` + strings.Join(buyTimes, ",") + `]; var buyPrices=[` + strings.Join(buyPrices, ",") + `];
var sellTimes=[` + strings.Join(sellTimes, ",") + `]; var sellPrices=[` + strings.Join(sellPrices, ",") + `];
var rsiCeil=` + f64(p.RsiCeil) + `; var rsiFloor=` + f64(p.RsiFloor) + `;
var traceCandle = { x: times, open: opens, high: highs, low: lows, close: closes, type: 'candlestick', xaxis: 'x', yaxis: 'y', name: 'Price', increasing: {line: {color: '#26a69a'}}, decreasing: {line: {color: '#ef5350'}} };
var traceHMA = { x: times, y: hma, type: 'scatter', xaxis: 'x', yaxis: 'y', name: 'HMA', line: {color: '#ffee58', width: 2} };
var traceBuy = { x: buyTimes, y: buyPrices, mode: 'markers', type: 'scatter', xaxis: 'x', yaxis: 'y', name: 'Buy', marker: {symbol: 'triangle-up', size: 14, color: '#26a69a', line: {color: '#fff', width: 1}} };
var traceSell = { x: sellTimes, y: sellPrices, mode: 'markers', type: 'scatter', xaxis: 'x', yaxis: 'y', name: 'Sell', marker: {symbol: 'triangle-down', size: 14, color: '#ef5350', line: {color: '#fff', width: 1}} };
var traceRSI = { x: times, y: rsi, type: 'scatter', xaxis: 'x2', yaxis: 'y2', name: 'RSI', line: {color: '#7c4dff', width: 2} };
var traceRSICeil = { x: times, y: times.map(function(){return rsiCeil}), type: 'scatter', xaxis: 'x2', yaxis: 'y2', name: 'Ceil ('+rsiCeil+')', line: {color: '#ef5350', width: 1, dash: 'dash'} };
var traceRSIFloor = { x: times, y: times.map(function(){return rsiFloor}), type: 'scatter', xaxis: 'x2', yaxis: 'y2', name: 'Floor ('+rsiFloor+')', line: {color: '#26a69a', width: 1, dash: 'dash'} };
var traceStochK = { x: times, y: stochK, type: 'scatter', xaxis: 'x3', yaxis: 'y3', name: 'Stoch %K', line: {color: '#42a5f5', width: 2} };
var traceStochD = { x: times, y: stochD, type: 'scatter', xaxis: 'x3', yaxis: 'y3', name: 'Stoch %D', line: {color: '#ffa726', width: 2} };
var traceUpper = { x: times, y: times.map(function(){return 80}), type: 'scatter', xaxis: 'x3', yaxis: 'y3', name: 'Overbought (80)', line: {color: '#ef5350', width: 1, dash: 'dash'} };
var traceLower = { x: times, y: times.map(function(){return 20}), type: 'scatter', xaxis: 'x3', yaxis: 'y3', name: 'Oversold (20)', line: {color: '#26a69a', width: 1, dash: 'dash'} };
var layout = {
  title: {text: '` + html.EscapeString(title) + `', font: {size: 14, color: '#ccc'}},
  paper_bgcolor: '#1a1a2e', plot_bgcolor: '#16213e', font: {color: '#aaa', size: 10},
  grid: {rows: 3, columns: 1, pattern: 'independent', roworder: 'top to bottom'},
  xaxis: {rangeslider: {visible: false}, gridcolor: '#2a2a4a', linecolor: '#333'},
  yaxis: {title: 'Price', gridcolor: '#2a2a4a', linecolor: '#333', side: 'right'},
  xaxis2: {title: 'Date', gridcolor: '#2a2a4a', linecolor: '#333'},
  yaxis2: {title: 'RSI', range: [0,100], gridcolor: '#2a2a4a', linecolor: '#333', side: 'right'},
  xaxis3: {title: 'Date', gridcolor: '#2a2a4a', linecolor: '#333'},
  yaxis3: {title: 'Stochastic', range: [0,100], gridcolor: '#2a2a4a', linecolor: '#333', side: 'right'},
  legend: {orientation: 'h', y: 1.02, font: {size: 9, color: '#aaa'}},
  dragmode: 'zoom', hovermode: 'x unified'
};
var traces = [traceCandle, traceHMA, traceBuy, traceSell, traceRSI, traceRSICeil, traceRSIFloor, traceStochK, traceStochD, traceUpper, traceLower];
Plotly.newPlot('chart', traces, layout, {responsive: true});
</script>
</body>
</html>`)
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func strategyLabel(id string) string {
	if s, ok := StrategyByName(id); ok {
		return s.Name
	}
	return id
}

func stamp() string {
	return time.Now().Format("20060102_150405")
}
