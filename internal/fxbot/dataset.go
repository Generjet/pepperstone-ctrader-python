package fxbot

import (
	"math"
	"time"
)

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
	HMA    float64
	Buy    int
	Sell   int
}

type Strategy struct {
	ID          string
	Name        string
	Description string
}

var strategies = []Strategy{
	{"rsi", "RSI only", "Buy when RSI crosses up through the oversold floor; sell when it crosses down through the overbought ceiling."},
	{"stoch", "Stochastic only", "Buy on bullish %K/%D crossover; sell on bearish %K/%D crossover."},
	{"rsi_stoch", "RSI + Stochastic", "Buy on bullish %K/%D crossover confirmed by RSI below floor; sell on bearish crossover confirmed by RSI above ceiling."},
	{"hma", "HMA trend", "Buy when price crosses above HMA; sell when price crosses below HMA."},
}

// Params holds all trading parameters for the bot.
type Params struct {
	Symbol      string  `json:"symbol"`
	Interval    string  `json:"interval"`
	Limit       int     `json:"limit"`
	Lots        float64 `json:"lots"`
	StrategyID  string  `json:"strategyId"`
	Mode        string  `json:"mode"`
	Script      string  `json:"script"`
	PollSeconds int     `json:"pollSeconds"`
	RsiPeriod   int     `json:"rsiPeriod"`
	RsiCeil     float64 `json:"rsiCeil"`
	RsiFloor    float64 `json:"rsiFloor"`
	KPeriod     int     `json:"kPeriod"`
	KSlow       int     `json:"kSlow"`
	DPeriod     int     `json:"dPeriod"`
	HMAPeriod   int     `json:"hmaPeriod"`
}

func DefaultParams() Params {
	return Params{
		Symbol:      "USDJPY",
		Interval:    "1h",
		Limit:       300,
		Lots:        0.01,
		StrategyID:  "rsi_stoch",
		Mode:        "demo",
		Script:      "trade_ejtrader_ct.py",
		PollSeconds: 60,
		RsiPeriod:   14,
		RsiCeil:     70,
		RsiFloor:    30,
		KPeriod:     14,
		KSlow:       3,
		DPeriod:     3,
		HMAPeriod:   9,
	}
}

func (p Params) FillDefaults() Params {
	d := DefaultParams()
	if p.Symbol == "" {
		p.Symbol = d.Symbol
	}
	if p.Interval == "" {
		p.Interval = d.Interval
	}
	if p.Limit <= 0 {
		p.Limit = d.Limit
	}
	if p.Lots <= 0 {
		p.Lots = d.Lots
	}
	if p.StrategyID == "" {
		p.StrategyID = d.StrategyID
	}
	if p.Mode == "" {
		p.Mode = d.Mode
	}
	if p.Script == "" {
		p.Script = d.Script
	}
	if p.PollSeconds <= 0 {
		p.PollSeconds = d.PollSeconds
	}
	if p.RsiPeriod <= 0 {
		p.RsiPeriod = d.RsiPeriod
	}
	if p.RsiCeil <= 0 {
		p.RsiCeil = d.RsiCeil
	}
	if p.RsiFloor <= 0 {
		p.RsiFloor = d.RsiFloor
	}
	if p.KPeriod <= 0 {
		p.KPeriod = d.KPeriod
	}
	if p.KSlow <= 0 {
		p.KSlow = d.KSlow
	}
	if p.DPeriod <= 0 {
		p.DPeriod = d.DPeriod
	}
	if p.HMAPeriod <= 0 {
		p.HMAPeriod = d.HMAPeriod
	}
	return p
}

func StrategyByName(id string) (Strategy, bool) {
	for _, s := range strategies {
		if s.ID == id {
			return s, true
		}
	}
	return Strategy{}, false
}

// Analyze computes all indicators and strategy signals for a candle series.
func Analyze(candles []Candle, p Params) []DataRow {
	n := len(candles)
	closes := make([]float64, n)
	highs := make([]float64, n)
	lows := make([]float64, n)
	for i, c := range candles {
		closes[i] = c.Close
		highs[i] = c.High
		lows[i] = c.Low
	}

	rsi := calculateRSI(closes, p.RsiPeriod)
	stK, stD := calculateStochastic(highs, lows, closes, p.KPeriod, p.KSlow, p.DPeriod)
	hull := hma(closes, p.HMAPeriod)

	rows := make([]DataRow, n)
	for i := range candles {
		rows[i] = DataRow{
			Time:   candles[i].Time,
			Open:   candles[i].Open,
			High:   candles[i].High,
			Low:    candles[i].Low,
			Close:  candles[i].Close,
			Volume: candles[i].Volume,
			RSI:    rsi[i],
			StochK: stK[i],
			StochD: stD[i],
			HMA:    hull[i],
		}
	}
	ApplySignals(rows, p.StrategyID, p)
	return rows
}

// ApplySignals marks Buy/Sell on each row according to the chosen strategy.
// A row evaluates against its previous row, so Buy/Sell are discrete events.
func ApplySignals(rows []DataRow, strategy string, p Params) {
	for i := range rows {
		rows[i].Buy, rows[i].Sell = 0, 0
	}
	for i := 1; i < len(rows); i++ {
		curr, prev := rows[i], rows[i-1]
		switch strategy {
		case "rsi":
			if math.IsNaN(curr.RSI) || math.IsNaN(prev.RSI) {
				continue
			}
			if prev.RSI <= p.RsiFloor && curr.RSI > p.RsiFloor {
				rows[i].Buy = 1
			}
			if prev.RSI >= p.RsiCeil && curr.RSI < p.RsiCeil {
				rows[i].Sell = 1
			}
		case "stoch":
			if math.IsNaN(curr.StochK) || math.IsNaN(curr.StochD) ||
				math.IsNaN(prev.StochK) || math.IsNaN(prev.StochD) {
				continue
			}
			if prev.StochK <= prev.StochD && curr.StochK > curr.StochD {
				rows[i].Buy = 1
			}
			if prev.StochK >= prev.StochD && curr.StochK < curr.StochD {
				rows[i].Sell = 1
			}
		case "rsi_stoch":
			if math.IsNaN(curr.StochK) || math.IsNaN(curr.StochD) ||
				math.IsNaN(prev.StochK) || math.IsNaN(prev.StochD) ||
				math.IsNaN(curr.RSI) {
				continue
			}
			if prev.StochK <= prev.StochD && curr.StochK > curr.StochD && curr.RSI < p.RsiFloor {
				rows[i].Buy = 1
			}
			if prev.StochK >= prev.StochD && curr.StochK < curr.StochD && curr.RSI > p.RsiCeil {
				rows[i].Sell = 1
			}
		case "hma":
			if math.IsNaN(curr.HMA) || math.IsNaN(prev.HMA) {
				continue
			}
			if prev.Close <= prev.HMA && curr.Close > curr.HMA {
				rows[i].Buy = 1
			}
			if prev.Close >= prev.HMA && curr.Close < curr.HMA {
				rows[i].Sell = 1
			}
		}
	}
}
