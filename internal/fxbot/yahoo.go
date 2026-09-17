package fxbot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Candle struct {
	Time   time.Time
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

var intervalSeconds = map[string]int64{
	"1m":  60,
	"5m":  300,
	"15m": 900,
	"30m": 1800,
	"1h":  3600,
	"1d":  86400,
	"1wk": 604800,
	"1mo": 2592000,
}

var intervalNames = []string{"1m", "5m", "15m", "30m", "1h", "1d", "1wk", "1mo"}

// yahooSymbols returns the Yahoo Finance symbol candidates for a forex pair
// like "USDJPY" (-> "JPY=X") or "EURUSD" (-> "EURUSD=X").
func yahooSymbols(pair string) []string {
	p := strings.ToUpper(strings.TrimSpace(pair))
	re := regexp.MustCompile(`^([A-Z]{3})([A-Z]{3})$`)
	var cands []string
	if m := re.FindStringSubmatch(p); m != nil {
		base, quote := m[1], m[2]
		if base == "USD" && quote != "USD" {
			cands = append(cands, quote+"=X")
		}
		cands = append(cands, p+"=X")
	} else {
		cands = append(cands, p+"=X")
	}
	seen := map[string]bool{}
	out := cands[:0]
	for _, c := range cands {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

type yahooResp struct {
	Chart struct {
		Result []struct {
			Timestamp  []int64 `json:"timestamp"`
			Indicators struct {
				Quote []struct {
					Open   []*float64 `json:"open"`
					High   []*float64 `json:"high"`
					Low    []*float64 `json:"low"`
					Close  []*float64 `json:"close"`
					Volume []*float64 `json:"volume"`
				} `json:"quote"`
			} `json:"indicators"`
		} `json:"result"`
		Error *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"chart"`
}

func FetchCandles(ctx context.Context, symbol, interval string, limit int) ([]Candle, error) {
	secs, ok := intervalSeconds[interval]
	if !ok {
		return nil, fmt.Errorf("unsupported interval %q", interval)
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 2000 {
		limit = 2000
	}
	period2 := time.Now().Unix()
	period1 := period2 - secs*int64(limit) - secs*4
	client := &http.Client{Timeout: 30 * time.Second}

	var lastErr error
	for _, sym := range yahooSymbols(symbol) {
		u := fmt.Sprintf(
			"https://query1.finance.yahoo.com/v8/finance/chart/%s?interval=%s&period1=%d&period2=%d&events=history",
			sym, interval, period1, period2,
		)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 FXBot/1.0")
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		var out yahooResp
		decodeErr := json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		if decodeErr != nil {
			lastErr = decodeErr
			continue
		}
		if resp.StatusCode != 200 {
			lastErr = fmt.Errorf("yahoo status %d for %s", resp.StatusCode, sym)
			continue
		}
		if out.Chart.Error != nil {
			lastErr = fmt.Errorf("yahoo error %s: %s", out.Chart.Error.Code, out.Chart.Error.Description)
			continue
		}
		if len(out.Chart.Result) == 0 {
			lastErr = fmt.Errorf("no result from yahoo for %s", sym)
			continue
		}
		res := out.Chart.Result[0]
		if len(res.Indicators.Quote) == 0 {
			lastErr = fmt.Errorf("no quote indicators from yahoo for %s", sym)
			continue
		}
		q := res.Indicators.Quote[0]
		ts := res.Timestamp
		candles := make([]Candle, 0, len(ts))
		for i := range ts {
			if i >= len(q.Open) || i >= len(q.High) || i >= len(q.Low) || i >= len(q.Close) {
				break
			}
			if q.Open[i] == nil || q.High[i] == nil || q.Low[i] == nil || q.Close[i] == nil {
				continue
			}
			var vol float64
			if i < len(q.Volume) && q.Volume[i] != nil {
				vol = *q.Volume[i]
			}
			candles = append(candles, Candle{
				Time:   time.Unix(ts[i], 0),
				Open:   *q.Open[i],
				High:   *q.High[i],
				Low:    *q.Low[i],
				Close:  *q.Close[i],
				Volume: vol,
			})
		}
		if len(candles) == 0 {
			lastErr = fmt.Errorf("no candle data from yahoo for %s", sym)
			continue
		}
		sort.Slice(candles, func(i, j int) bool { return candles[i].Time.Before(candles[j].Time) })
		if len(candles) > limit {
			candles = candles[len(candles)-limit:]
		}
		return candles, nil
	}
	return nil, fmt.Errorf("could not fetch %s from Yahoo (tried %s): %w",
		symbol, strings.Join(yahooSymbols(symbol), ","), lastErr)
}
