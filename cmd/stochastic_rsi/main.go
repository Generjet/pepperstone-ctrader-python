package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"html"
	"log"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/adshao/go-binance/v2"
)

type Candle struct {
	Time   time.Time
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

type DataRow struct {
	Time   time.Time
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
	RSI    float64
	StochK float64
	StochD float64
	Buy    int
	Sell   int
}

func main() {
	currency := flag.String("currency", "ETHUSDT", "Trading pair symbol")
	interval := flag.String("interval", "1h", "Kline interval")
	limit := flag.Int("limit", 200, "Number of candles to fetch")
	kPeriod := flag.Int("k-period", 14, "Stochastic %K lookback period")
	kSmooth := flag.Int("k-smooth", 3, "Stochastic %K smoothing (slow) period")
	dPeriod := flag.Int("d-period", 3, "Stochastic %D moving average period")
	rsiPeriod := flag.Int("rsi-period", 14, "RSI calculation period")
	rsiCeil := flag.Float64("rsi-ceil", 70, "RSI overbought level for sell confirmation")
	rsiFloor := flag.Float64("rsi-floor", 30, "RSI oversold level for buy confirmation")
	output := flag.String("output", "", "Output CSV path (default: stochastic_rsi_<datetime>.csv)")
	chart := flag.String("chart", "", "Output HTML chart path (default: stochastic_rsi_<datetime>.html)")
	flag.Parse()

	now := time.Now().Format("20060102_150405")
	if *output == "" {
		*output = fmt.Sprintf("stochastic_rsi_%s.csv", now)
	}
	if *chart == "" {
		*chart = fmt.Sprintf("stochastic_rsi_%s.html", now)
	}

	client := binance.NewClient(os.Getenv("BINANCE_API_KEY"), os.Getenv("BINANCE_SECRET_KEY"))
	ctx := context.Background()

	needed := *limit + *kPeriod + *rsiPeriod + 50
	klines, err := client.NewKlinesService().
		Symbol(*currency).
		Interval(*interval).
		Limit(needed).
		Do(ctx)
	if err != nil {
		log.Fatalf("Failed to fetch klines: %v", err)
	}
	if len(klines) == 0 {
		log.Fatalf("No klines returned")
	}

	candles := make([]Candle, len(klines))
	for i, k := range klines {
		candles[i] = Candle{
			Time:   time.UnixMilli(k.OpenTime),
			Open:   mustParse(k.Open),
			High:   mustParse(k.High),
			Low:    mustParse(k.Low),
			Close:  mustParse(k.Close),
			Volume: mustParse(k.Volume),
		}
	}
	sort.Slice(candles, func(i, j int) bool { return candles[i].Time.Before(candles[j].Time) })

	closes := floatsFromCandles(candles, func(c Candle) float64 { return c.Close })
	highs := floatsFromCandles(candles, func(c Candle) float64 { return c.High })
	lows := floatsFromCandles(candles, func(c Candle) float64 { return c.Low })

	rsi := calculateRSI(closes, *rsiPeriod)
	stochK, stochD := calculateStochastic(highs, lows, closes, *kPeriod, *kSmooth, *dPeriod)

	rows := make([]DataRow, len(candles))
	for i := range candles {
		rows[i] = DataRow{
			Time:   candles[i].Time,
			Open:   candles[i].Open,
			High:   candles[i].High,
			Low:    candles[i].Low,
			Close:  candles[i].Close,
			Volume: candles[i].Volume,
			RSI:    rsi[i],
			StochK: stochK[i],
			StochD: stochD[i],
		}
	}

	generateSignals(rows, *rsiCeil, *rsiFloor)

	writeCSV(*output, rows)
	fmt.Printf("CSV saved: %s\n", *output)

	if err := writeChart(*chart, *currency, *interval, rows, *rsiCeil, *rsiFloor); err != nil {
		log.Fatalf("Failed to generate chart: %v", err)
	}
	fmt.Printf("Chart saved: %s\n", *chart)
}

func mustParse(s string) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return math.NaN()
	}
	return f
}

func floatsFromCandles(candles []Candle, fn func(Candle) float64) []float64 {
	r := make([]float64, len(candles))
	for i, c := range candles {
		r[i] = fn(c)
	}
	return r
}

func sma(values []float64, period int) []float64 {
	r := make([]float64, len(values))
	for i := range r {
		r[i] = math.NaN()
	}
	if period <= 0 || len(values) < period {
		return r
	}
	first := -1
	for i, v := range values {
		if !math.IsNaN(v) {
			first = i
			break
		}
	}
	if first < 0 || first+period > len(values) {
		return r
	}
	sum := 0.0
	for i := first; i < first+period; i++ {
		sum += values[i]
	}
	r[first+period-1] = sum / float64(period)
	for i := first + period; i < len(values); i++ {
		sum = sum - values[i-period] + values[i]
		r[i] = sum / float64(period)
	}
	return r
}

func calculateRSI(closes []float64, period int) []float64 {
	r := make([]float64, len(closes))
	for i := range r {
		r[i] = math.NaN()
	}
	if len(closes) < period+1 {
		return r
	}

	gainSum, lossSum := 0.0, 0.0
	for i := 1; i <= period; i++ {
		diff := closes[i] - closes[i-1]
		if diff > 0 {
			gainSum += diff
		} else {
			lossSum -= diff
		}
	}

	avgGain := gainSum / float64(period)
	avgLoss := lossSum / float64(period)
	if avgLoss == 0 {
		r[period] = 100
	} else {
		r[period] = 100 - 100/(1+avgGain/avgLoss)
	}

	for i := period + 1; i < len(closes); i++ {
		diff := closes[i] - closes[i-1]
		gain, loss := 0.0, 0.0
		if diff > 0 {
			gain = diff
		} else {
			loss = -diff
		}
		avgGain = (avgGain*float64(period-1) + gain) / float64(period)
		avgLoss = (avgLoss*float64(period-1) + loss) / float64(period)
		if avgLoss == 0 {
			r[i] = 100
		} else {
			r[i] = 100 - 100/(1+avgGain/avgLoss)
		}
	}
	return r
}

func calculateStochastic(highs, lows, closes []float64, kPeriod, kSmooth, dPeriod int) ([]float64, []float64) {
	n := len(closes)
	rawK := make([]float64, n)
	for i := range rawK {
		rawK[i] = math.NaN()
	}

	for i := kPeriod - 1; i < n; i++ {
		hh := highs[i-kPeriod+1]
		ll := lows[i-kPeriod+1]
		for j := i - kPeriod + 2; j <= i; j++ {
			if highs[j] > hh {
				hh = highs[j]
			}
			if lows[j] < ll {
				ll = lows[j]
			}
		}
		if hh == ll {
			rawK[i] = 50
		} else {
			rawK[i] = 100 * (closes[i] - ll) / (hh - ll)
		}
	}

	smoothedK := sma(rawK, kSmooth)
	stochD := sma(smoothedK, dPeriod)

	return smoothedK, stochD
}

func generateSignals(rows []DataRow, rsiCeil, rsiFloor float64) {
	for i := 1; i < len(rows); i++ {
		curr, prev := rows[i], rows[i-1]
		if math.IsNaN(curr.StochK) || math.IsNaN(curr.StochD) ||
			math.IsNaN(prev.StochK) || math.IsNaN(prev.StochD) ||
			math.IsNaN(curr.RSI) {
			continue
		}
		if prev.StochK <= prev.StochD && curr.StochK > curr.StochD && curr.RSI < rsiFloor {
			rows[i].Buy = 1
		}
		if prev.StochK >= prev.StochD && curr.StochK < curr.StochD && curr.RSI > rsiCeil {
			rows[i].Sell = 1
		}
	}
}

func writeCSV(path string, rows []DataRow) {
	f, err := os.Create(path)
	if err != nil {
		log.Fatalf("Cannot create CSV: %v", err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	w.Write([]string{"time", "open", "high", "low", "close", "volume", "rsi", "stoch_k", "stoch_d", "buy", "sell"})

	for _, row := range rows {
		timeStr := row.Time.Format("2006-01-02 15:04:05")
		rec := []string{
			timeStr,
			f64s(row.Open),
			f64s(row.High),
			f64s(row.Low),
			f64s(row.Close),
			f64s(row.Volume),
			f64sNaN(row.RSI),
			f64sNaN(row.StochK),
			f64sNaN(row.StochD),
			fmt.Sprintf("%d", row.Buy),
			fmt.Sprintf("%d", row.Sell),
		}
		w.Write(rec)
	}
}

func f64s(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func f64sNaN(v float64) string {
	if math.IsNaN(v) {
		return ""
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func writeChart(path, currency, interval string, rows []DataRow, rsiCeil, rsiFloor float64) error {
	var buf bytes.Buffer

	timeFmt := "2006-01-02 15:04:05"
	var timeStrs []string
	var opens, highs, lows, closes []string
	var rsiVals, stochKVals, stochDVals []string
	var buyTimes, buyPrices []string
	var sellTimes, sellPrices []string

	for _, r := range rows {
		if math.IsNaN(r.RSI) || math.IsNaN(r.StochK) || math.IsNaN(r.StochD) {
			continue
		}
		ts := fmt.Sprintf(`"%s"`, r.Time.Format(timeFmt))
		timeStrs = append(timeStrs, ts)
		opens = append(opens, f64s(r.Open))
		highs = append(highs, f64s(r.High))
		lows = append(lows, f64s(r.Low))
		closes = append(closes, f64s(r.Close))
		rsiVals = append(rsiVals, f64s(r.RSI))
		stochKVals = append(stochKVals, f64s(r.StochK))
		stochDVals = append(stochDVals, f64s(r.StochD))
		if r.Buy == 1 {
			buyTimes = append(buyTimes, ts)
			buyPrices = append(buyPrices, f64s(r.Close))
		}
		if r.Sell == 1 {
			sellTimes = append(sellTimes, ts)
			sellPrices = append(sellPrices, f64s(r.Close))
		}
	}

	title := fmt.Sprintf("%s (%s) — RSI + Stochastic Analysis", currency, interval)

	writeHTMLHead(&buf, title)
	writeTracesJS(&buf, timeStrs, opens, highs, lows, closes, rsiVals, stochKVals, stochDVals, buyTimes, buyPrices, sellTimes, sellPrices, rsiCeil, rsiFloor)
	writeLayoutJS(&buf, title)
	writeHTMLEnd(&buf)

	return os.WriteFile(path, buf.Bytes(), 0644)
}

func writeHTMLHead(buf *bytes.Buffer, title string) {
	fmt.Fprintf(buf, `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>%s</title>
<script src="https://cdn.plot.ly/plotly-2.35.2.min.js"></script>
<style>
* { margin: 0; padding: 0; box-sizing: border-box; }
body { background: #1a1a2e; color: #eee; font-family: 'Segoe UI', Arial, sans-serif; padding: 20px; }
h1 { text-align: center; margin-bottom: 16px; font-size: 18px; color: #ccc; }
#chart { width: 100%%; height: 100vh; }
</style>
</head>
<body>
<h1>%s</h1>
<div id="chart"></div>
<script>
`, html.EscapeString(title), html.EscapeString(title))
}

func writeTracesJS(buf *bytes.Buffer, times, opens, highs, lows, closes, rsi, stochK, stochD []string, buyTimes, buyPrices, sellTimes, sellPrices []string, rsiCeil, rsiFloor float64) {
	join := func(ss []string) string { return strings.Join(ss, ",") }

	fmt.Fprintf(buf, `var times=[%s];
var opens=[%s]; var highs=[%s]; var lows=[%s]; var closes=[%s];
var rsi=[%s]; var stochK=[%s]; var stochD=[%s];
var buyTimes=[%s]; var buyPrices=[%s];
var sellTimes=[%s]; var sellPrices=[%s];
`,
		join(times), join(opens), join(highs), join(lows), join(closes),
		join(rsi), join(stochK), join(stochD),
		join(buyTimes), join(buyPrices),
		join(sellTimes), join(sellPrices))

	fmt.Fprintf(buf, `
var rsiCeil=%v; var rsiFloor=%v;
`, rsiCeil, rsiFloor)

	buf.WriteString(`
var traceCandle = {
    x: times, open: opens, high: highs, low: lows, close: closes,
    type: 'candlestick', xaxis: 'x', yaxis: 'y',
    name: 'Price', increasing: {line: {color: '#26a69a'}},
    decreasing: {line: {color: '#ef5350'}}
};

var traceBuy = {
    x: buyTimes, y: buyPrices,
    mode: 'markers', type: 'scatter', xaxis: 'x', yaxis: 'y',
    name: 'Buy', marker: {symbol: 'triangle-up', size: 14, color: '#26a69a', line: {color: '#fff', width: 1}}
};

var traceSell = {
    x: sellTimes, y: sellPrices,
    mode: 'markers', type: 'scatter', xaxis: 'x', yaxis: 'y',
    name: 'Sell', marker: {symbol: 'triangle-down', size: 14, color: '#ef5350', line: {color: '#fff', width: 1}}
};

var traceRSI = {
    x: times, y: rsi, type: 'scatter', xaxis: 'x2', yaxis: 'y2',
    name: 'RSI', line: {color: '#7c4dff', width: 2}
};

var traceRSICeil = {
    x: times, y: times.map(function(){return rsiCeil}),
    type: 'scatter', xaxis: 'x2', yaxis: 'y2',
    name: 'Ceil ('+rsiCeil+')', line: {color: '#ef5350', width: 1, dash: 'dash'}
};

var traceRSIFloor = {
    x: times, y: times.map(function(){return rsiFloor}),
    type: 'scatter', xaxis: 'x2', yaxis: 'y2',
    name: 'Floor ('+rsiFloor+')', line: {color: '#26a69a', width: 1, dash: 'dash'}
};

var traceStochK = {
    x: times, y: stochK, type: 'scatter', xaxis: 'x3', yaxis: 'y3',
    name: 'Stoch %K', line: {color: '#42a5f5', width: 2}
};

var traceStochD = {
    x: times, y: stochD, type: 'scatter', xaxis: 'x3', yaxis: 'y3',
    name: 'Stoch %D', line: {color: '#ffa726', width: 2}
};

var traceUpper = {
    x: times, y: times.map(function(){return 80}),
    type: 'scatter', xaxis: 'x3', yaxis: 'y3',
    name: 'Overbought (80)', line: {color: '#ef5350', width: 1, dash: 'dash'}
};

var traceLower = {
    x: times, y: times.map(function(){return 20}),
    type: 'scatter', xaxis: 'x3', yaxis: 'y3',
    name: 'Oversold (20)', line: {color: '#26a69a', width: 1, dash: 'dash'}
};
`)
}

func writeLayoutJS(buf *bytes.Buffer, title string) {
	fmt.Fprintf(buf, `
var layout = {
    title: {text: '%s', font: {size: 14, color: '#ccc'}},
    paper_bgcolor: '#1a1a2e', plot_bgcolor: '#16213e',
    font: {color: '#aaa', size: 10},
    grid: {rows: 3, columns: 1, pattern: 'independent', roworder: 'top to bottom'},
    xaxis: {rangeslider: {visible: false}, gridcolor: '#2a2a4a', linecolor: '#333'},
    yaxis: {title: 'Price', gridcolor: '#2a2a4a', linecolor: '#333', side: 'right'},
    xaxis2: {title: 'Date', gridcolor: '#2a2a4a', linecolor: '#333'},
    yaxis2: {title: 'RSI', range: [0,100], gridcolor: '#2a2a4a', linecolor: '#333', side: 'right'},
    xaxis3: {title: 'Date', gridcolor: '#2a2a4a', linecolor: '#333'},
    yaxis3: {title: 'Stochastic', range: [0,100], gridcolor: '#2a2a4a', linecolor: '#333', side: 'right'},
    legend: {orientation: 'h', y: 1.02, font: {size: 9, color: '#aaa'}},
    dragmode: 'zoom',
    hovermode: 'x unified'
};
`, html.EscapeString(title))
}

func writeHTMLEnd(buf *bytes.Buffer) {
	buf.WriteString(`
var traces = [traceCandle, traceBuy, traceSell, traceRSI, traceRSICeil, traceRSIFloor, traceStochK, traceStochD, traceUpper, traceLower];
Plotly.newPlot('chart', traces, layout, {responsive: true});
</script>
</body>
</html>`)
}
