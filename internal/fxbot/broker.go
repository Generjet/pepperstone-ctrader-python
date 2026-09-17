package fxbot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var allowedScripts = map[string]bool{
	"trade_ejtrader_ct.py": true,
	"trade_sinan_ct.py":    true,
	"trade_ctrader_fix.py": true,
}

var scriptNames = []string{"trade_ejtrader_ct.py", "trade_sinan_ct.py", "trade_ctrader_fix.py"}

var lotScale = map[string]float64{
	"BTCUSD": 1, "ETHUSD": 1,
	"XAUUSD": 100, "XAUEUR": 100,
	"XAGUSD": 5000, "XAGEUR": 5000,
	"ADAUSD": 100, "XRPUSD": 100, "DOGEUSD": 100, "SOLUSD": 100,
	"AVAXUSD": 100, "DOTUSD": 100, "LINKUSD": 100, "MATICUSD": 100,
	"SHIBUSD": 100, "LTCUSD": 100, "BCHUSD": 100, "UNIUSD": 100,
	"AAVEUSD": 100, "ATOMUSD": 100, "FILUSD": 100,
}

func unitsPerLot(symbol string) float64 {
	if v, ok := lotScale[strings.ToUpper(symbol)]; ok {
		return v
	}
	return 100_000
}

// Broker shells out to the Pepperstone cTrader Python scripts.
type Broker struct {
	Dir     string
	Python  string
	Script  string
	Mode    string
	Timeout time.Duration
}

type Quote struct {
	Bid float64
	Ask float64
}

type OrderResult struct {
	Ticket string
	Bid    float64
	Ask    float64
	Price  float64
	Output string
}

// DetectPython finds the venv interpreter next to the scripts, falling back to
// a system python3.
func DetectPython(dir string) string {
	candidates := []string{
		filepath.Join(dir, "venv", "bin", "python3"),
		filepath.Join(dir, "venv", "bin", "python"),
		filepath.Join(dir, "venv", "Scripts", "python.exe"),
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c
		}
	}
	return "python3"
}

func (b *Broker) baseArgs() []string {
	return []string{b.Script, "--mode", b.Mode}
}

func (b *Broker) run(ctx context.Context, extra ...string) (string, error) {
	timeout := b.Timeout
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := b.baseArgs()
	args = append(args, extra...)
	cmd := exec.CommandContext(runCtx, b.Python, args...)
	cmd.Dir = b.Dir
	out, err := cmd.CombinedOutput()
	text := string(out)

	if runCtx.Err() == context.DeadlineExceeded {
		return text, fmt.Errorf("broker script timed out after %s (output):\n%s", timeout, text)
	}
	if err != nil {
		return text, fmt.Errorf("broker script failed: %v (output):\n%s", err, text)
	}
	return text, nil
}

// extractErrors pulls any failure markers out of the script output.
func extractErrors(text string) []string {
	var errs []string
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[ERROR]") ||
			strings.HasPrefix(t, "[SEND ERROR]") ||
			strings.Contains(t, "Order rejected") {
			errs = append(errs, t)
		}
	}
	return errs
}

var priceRe = regexp.MustCompile(`(?i)\bbid=([0-9.]+).*ask=([0-9.]+)`)
var ticketRe = regexp.MustCompile(`(?i)\bticket=(\S+)`)

func parseQuote(text string) Quote {
	if m := priceRe.FindStringSubmatch(text); m != nil {
		bid, _ := strconv.ParseFloat(m[1], 64)
		ask, _ := strconv.ParseFloat(m[2], 64)
		return Quote{Bid: bid, Ask: ask}
	}
	return Quote{}
}

func parseTicket(text string) string {
	if m := ticketRe.FindStringSubmatch(text); m != nil {
		return m[1]
	}
	return ""
}

func (b *Broker) Quote(ctx context.Context, symbol string) (Quote, error) {
	text, err := b.run(ctx, "--price-only", "--currency", strings.ToUpper(symbol))
	if errs := extractErrors(text); len(errs) > 0 {
		return Quote{}, errors.New(strings.Join(errs, " | "))
	}
	if err != nil {
		return Quote{}, err
	}
	q := parseQuote(text)
	if q.Bid <= 0 || q.Ask <= 0 {
		return Quote{}, fmt.Errorf("no bid/ask in script output:\n%s", text)
	}
	return q, nil
}

func (b *Broker) Order(ctx context.Context, side, symbol string, lots, sl, tp float64) (OrderResult, error) {
	args := []string{
		"--side", side,
		"--amount", strconv.FormatFloat(lots, 'f', -1, 64),
		"--currency", strings.ToUpper(symbol),
	}
	if sl > 0 {
		args = append(args, "--sl", strconv.FormatFloat(sl, 'f', -1, 64))
	}
	if tp > 0 {
		args = append(args, "--tp", strconv.FormatFloat(tp, 'f', -1, 64))
	}

	text, err := b.run(ctx, args...)
	res := OrderResult{Output: text}

	var errs []string
	if e := extractErrors(text); len(e) > 0 {
		errs = append(errs, e...)
	}
	if err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return res, errors.New(strings.Join(errs, " | "))
	}

	res.Ticket = parseTicket(text)
	q := parseQuote(text)
	res.Bid, res.Ask = q.Bid, q.Ask
	if side == "buy" {
		res.Price = q.Ask
	} else {
		res.Price = q.Bid
	}
	if res.Price <= 0 {
		return res, errors.New("no fill price in script output")
	}
	return res, nil
}
