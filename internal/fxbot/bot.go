package fxbot

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Files struct {
	CSV  string `json:"csv,omitempty"`
	HTML string `json:"html,omitempty"`
}

type Status struct {
	Running    bool    `json:"running"`
	Params     Params  `json:"params"`
	LastCheck  string  `json:"lastCheck,omitempty"`
	LastPrice  float64 `json:"lastPrice"`
	LastBid    float64 `json:"lastBid"`
	LastAsk    float64 `json:"lastAsk"`
	LastSignal string  `json:"lastSignal,omitempty"`
	LastError  string  `json:"lastError,omitempty"`
	OpenTrade  *Trade  `json:"openTrade"`
	Files      Files   `json:"files"`
}

type Bot struct {
	mu        sync.Mutex
	running   bool
	cancel    context.CancelFunc
	wait      sync.WaitGroup
	params    Params
	broker    *Broker
	trades    *TradeStore
	hub       *Hub
	logbuf    *LogBuf
	dataDir   string
	pepperDir string
	status    Status
	rows      []DataRow
	lastEval  time.Time
}

func NewBot(dataDir, pepperDir string, hub *Hub, logbuf *LogBuf) *Bot {
	return &Bot{
		trades:    LoadTradeStore(filepath.Join(dataDir, "trades.json")),
		hub:       hub,
		logbuf:    logbuf,
		dataDir:   dataDir,
		pepperDir: pepperDir,
	}
}

func (b *Bot) logf(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	b.logbuf.Add(msg)
	b.hub.Publish("log", LogLine{At: nowStamp(), Msg: msg})
	log.Println(msg)
}

func (b *Bot) publishStatus() {
	b.hub.Publish("status", b.Status())
}

func (b *Bot) publishTrades() {
	b.hub.Publish("trades", b.trades.List())
}

func (b *Bot) setLastError(err error) {
	b.mu.Lock()
	b.status.LastError = err.Error()
	b.mu.Unlock()
	b.publishStatus()
}

var symbolRe = regexp.MustCompile(`^[A-Za-z]{6}$`)

func (b *Bot) Start(p Params) error {
	p = p.FillDefaults()
	p.Symbol = strings.ToUpper(p.Symbol)
	if !symbolRe.MatchString(p.Symbol) {
		return errors.New("symbol must be 6 letters, e.g. USDJPY")
	}
	if _, ok := intervalSeconds[p.Interval]; !ok {
		return fmt.Errorf("unsupported interval %q", p.Interval)
	}
	if _, ok := StrategyByName(p.StrategyID); !ok {
		return fmt.Errorf("unknown strategy %q", p.StrategyID)
	}
	if p.Mode != "live" && p.Mode != "demo" {
		return fmt.Errorf("mode must be live or demo, got %q", p.Mode)
	}
	if !allowedScripts[p.Script] {
		return fmt.Errorf("unsupported script %q", p.Script)
	}
	if p.PollSeconds < 5 {
		p.PollSeconds = 5
	}
	if fi, err := os.Stat(b.pepperDir); err != nil || !fi.IsDir() {
		return fmt.Errorf("pepperstone scripts dir not found at %s", b.pepperDir)
	}
	if _, err := os.Stat(filepath.Join(b.pepperDir, p.Script)); err != nil {
		return fmt.Errorf("python script %s not found in %s", p.Script, b.pepperDir)
	}

	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		return errors.New("bot is already running; stop it first")
	}
	b.running = true
	b.params = p
	b.lastEval = time.Time{}
	b.status = Status{Running: true, Params: p}
	ctx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel
	b.broker = &Broker{
		Dir:     b.pepperDir,
		Python:  DetectPython(b.pepperDir),
		Script:  p.Script,
		Mode:    p.Mode,
		Timeout: 90 * time.Second,
	}
	b.mu.Unlock()

	if open := b.trades.OpenTrade(); open != nil {
		b.logf("[POSITION] existing open position: %s %.2f lots bought at %.5f",
			open.Symbol, open.Lots, open.BuyPrice)
	}

	b.wait.Add(1)
	go func() {
		defer b.wait.Done()
		b.runLoop(ctx)
	}()
	b.logf("[BOT] started %s %s | %s | %s mode | %.4g lots | %ss polling",
		p.Symbol, p.Interval, strategyLabel(p.StrategyID), p.Mode, p.Lots,
		fmt.Sprint(p.PollSeconds))
	b.publishStatus()
	return nil
}

func (b *Bot) Stop() {
	b.mu.Lock()
	if !b.running {
		b.mu.Unlock()
		return
	}
	b.running = false
	b.cancel()
	b.mu.Unlock()
	b.wait.Wait()
	b.mu.Lock()
	b.status.Running = false
	b.mu.Unlock()
	b.logf("[BOT] stopped")
	b.publishStatus()
}

func (b *Bot) Status() Status {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.status
	if t := b.trades.OpenTrade(); t != nil {
		cp := t
		s.OpenTrade = cp
	}
	return s
}

func (b *Bot) Trades() []Trade {
	return b.trades.List()
}

func (b *Bot) RowsFor(symbol, interval string) []DataRow {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.running && b.params.Symbol == symbol && b.params.Interval == interval {
		return b.rows
	}
	return nil
}

func (b *Bot) Params() Params {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.params
}

func (b *Bot) runLoop(ctx context.Context) {
	for {
		b.tick(ctx)
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(b.pollSeconds()) * time.Second):
		}
	}
}

func (b *Bot) pollSeconds() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.params.PollSeconds < 5 {
		return 5
	}
	return b.params.PollSeconds
}

func (b *Bot) tick(ctx context.Context) {
	b.mu.Lock()
	p := b.params
	b.mu.Unlock()

	b.logf("[DATA] fetching %s (%s) from Yahoo…", p.Symbol, p.Interval)
	candles, err := FetchCandles(ctx, p.Symbol, p.Interval, p.Limit)
	if err != nil {
		b.logf("[DATA] ERROR: %v", err)
		b.setLastError(err)
		return
	}
	rows := Analyze(candles, p)

	b.mu.Lock()
	b.rows = rows
	b.status.LastCheck = nowStamp()
	if len(rows) > 0 {
		b.status.LastPrice = rows[len(rows)-1].Close
	}
	b.mu.Unlock()

	// Evaluate the newest fully-formed candle (the final row is still forming).
	if len(rows) >= 3 {
		cand := rows[len(rows)-2]
		b.mu.Lock()
		changed := cand.Time.After(b.lastEval)
		if changed {
			b.lastEval = cand.Time
		}
		b.mu.Unlock()
		if changed {
			b.decide(ctx, &cand, rows[len(rows)-1].Close)
		} else {
			b.logf("[SIGNAL] no new completed candle (last %s)", cand.Time.Format("2006-01-02 15:04"))
		}
	}

	b.hub.Publish("data", map[string]any{"symbol": p.Symbol, "interval": p.Interval})
	b.publishStatus()
}

func (b *Bot) decide(ctx context.Context, cand *DataRow, curClose float64) {
	b.mu.Lock()
	p := b.params
	b.mu.Unlock()

	ts := cand.Time.Format("2006-01-02 15:04")
	switch {
	case cand.Buy == 1:
		b.logf("[SIGNAL] BUY at %s (close %.5f)", ts, cand.Close)
		b.setSignal("BUY " + ts)
		if open := b.trades.OpenTrade(); open != nil {
			b.logf("[TRADE] already holding %s, ignoring BUY signal", open.Symbol)
			return
		}
		b.execute(ctx, p, "buy")
	case cand.Sell == 1:
		b.logf("[SIGNAL] SELL at %s (close %.5f)", ts, cand.Close)
		b.setSignal("SELL " + ts)
		open := b.trades.OpenTrade()
		if open == nil {
			b.logf("[TRADE] SELL signal but no open position, nothing to close")
			return
		}
		if !b.profitable(ctx, open) {
			return
		}
		b.execute(ctx, p, "sell")
	default:
		b.setSignal("none " + ts)
	}
}

func (b *Bot) setSignal(s string) {
	b.mu.Lock()
	b.status.LastSignal = s
	b.mu.Unlock()
}

// profitable checks the live broker bid/ask before closing: sell only if the
// current price (bid for a long) is higher than the buy price.
func (b *Bot) profitable(ctx context.Context, open *Trade) bool {
	q, err := b.broker.Quote(ctx, open.Symbol)
	if err != nil {
		b.logf("[TRADE] profit check failed: %v", err)
		b.setLastError(err)
		return false
	}
	b.mu.Lock()
	b.status.LastBid, b.status.LastAsk = q.Bid, q.Ask
	b.mu.Unlock()
	if q.Bid <= open.BuyPrice {
		b.logf("[TRADE] SELL skipped: current bid %.5f <= buy %.5f (not profitable)",
			q.Bid, open.BuyPrice)
		return false
	}
	return true
}

func (b *Bot) execute(ctx context.Context, p Params, side string) {
	res, err := b.broker.Order(ctx, side, p.Symbol, p.Lots, 0, 0)
	if err != nil {
		b.logf("[ORDER] %s FAILED for %s: %v", strings.ToUpper(side), p.Symbol, err)
		b.setLastError(err)
		return
	}
	if side == "buy" {
		t := Trade{
			ID:       newTradeID(p.Symbol),
			Symbol:   p.Symbol,
			Interval: p.Interval,
			Strategy: p.StrategyID,
			Mode:     p.Mode,
			Lots:     p.Lots,
			BuyDate:  time.Now(),
			BuyPrice: res.Price,
			Ticket:   res.Ticket,
			Status:   "open",
		}
		b.trades.Add(t)
		b.logf("[ORDER] BUY %s %.4g lots executed at %.5f (ticket %s)",
			p.Symbol, p.Lots, res.Price, res.Ticket)
	} else {
		open := b.trades.OpenTrade()
		if open == nil {
			b.logf("[ORDER] SELL requested but no open trade to close")
			return
		}
		closed := b.trades.Close(open.ID, res.Price, res.Ticket, time.Now())
		if closed == nil {
			b.logf("[ORDER] SELL: could not close trade %s", open.ID)
			return
		}
		b.logf("[ORDER] SELL %s %.4g lots executed at %.5f profit %+.5f pips (%+.2f%%)| ticket %s",
			p.Symbol, p.Lots, res.Price, closed.ProfitPoints, closed.ProfitPct, res.Ticket)
	}
	b.publishTrades()
	b.publishStatus()
}

// Export writes the current dataset to CSV and a standalone HTML report.
func (b *Bot) Export() (Files, error) {
	b.mu.Lock()
	p := b.params
	rows := b.rows
	b.mu.Unlock()

	if len(rows) == 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		candles, err := FetchCandles(ctx, p.Symbol, p.Interval, p.Limit)
		if err != nil {
			return Files{}, err
		}
		rows = Analyze(candles, p)
	}
	if err := os.MkdirAll(b.dataDir, 0o755); err != nil {
		return Files{}, err
	}
	st := stamp()
	name := fmt.Sprintf("%s_%s_%s", strings.ToUpper(p.Symbol), p.Interval, st)
	csvPath := filepath.Join(b.dataDir, name+".csv")
	htmlPath := filepath.Join(b.dataDir, name+".html")
	if err := writeCSV(csvPath, rows); err != nil {
		return Files{}, err
	}
	if err := WriteReport(htmlPath, p.Symbol, p.Interval, rows, p); err != nil {
		return Files{}, err
	}
	files := Files{CSV: csvPath, HTML: htmlPath}
	b.mu.Lock()
	b.status.Files = files
	b.mu.Unlock()
	b.logf("[EXPORT] saved %s and %s", csvPath, htmlPath)
	b.publishStatus()
	return files, nil
}
