package fxbot

import (
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"strconv"
)

func writeCSV(path string, rows []DataRow) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write([]string{"time", "open", "high", "low", "close", "volume", "rsi", "stoch_k", "stoch_d", "hma", "buy", "sell"}); err != nil {
		return err
	}
	for _, row := range rows {
		rec := []string{
			row.Time.Format("2006-01-02 15:04:05"),
			f64(row.Open),
			f64(row.High),
			f64(row.Low),
			f64(row.Close),
			f64(row.Volume),
			f64NaN(row.RSI),
			f64NaN(row.StochK),
			f64NaN(row.StochD),
			f64NaN(row.HMA),
			strconv.Itoa(row.Buy),
			strconv.Itoa(row.Sell),
		}
		if err := w.Write(rec); err != nil {
			return err
		}
	}
	return nil
}

func f64(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func f64NaN(v float64) string {
	if math.IsNaN(v) {
		return ""
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func fmtPrice(v float64) string {
	if math.IsNaN(v) {
		return "-"
	}
	return fmt.Sprintf("%g", v)
}
