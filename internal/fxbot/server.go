package fxbot

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

//go:embed web/index.html
var indexHTML string

type Server struct {
	bot *Bot
	hub *Hub
}

func NewServer(bot *Bot, hub *Hub) *Server {
	return &Server{bot: bot, hub: hub}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/meta", s.handleMeta)
	mux.HandleFunc("/api/data", s.handleData)
	mux.HandleFunc("/api/trades", s.handleTrades)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/start", s.handleStart)
	mux.HandleFunc("/api/stop", s.handleStop)
	mux.HandleFunc("/api/export", s.handleExport)
	mux.HandleFunc("/api/logs", s.handleLogs)
	mux.HandleFunc("/api/stream", s.handleStream)
	mux.HandleFunc("/files/", s.handleFiles)
	return mux
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(indexHTML))
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	symbols := []string{"USDJPY", "EURUSD", "GBPUSD", "AUDUSD", "USDCHF", "USDCAD", "NZDUSD", "EURGBP", "EURJPY"}
	strats := make([]map[string]string, 0, len(strategies))
	for _, st := range strategies {
		strats = append(strats, map[string]string{
			"id":          st.ID,
			"name":        st.Name,
			"description": st.Description,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"symbols":    symbols,
		"intervals":  intervalNames,
		"strategies": strats,
		"scripts":    scriptNames,
		"modes":      []string{"demo", "live"},
		"defaults":   DefaultParams(),
	})
}

func (s *Server) handleData(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	symbol := q.Get("symbol")
	interval := q.Get("interval")
	limit := 500
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	params := DefaultParams()
	if symbol != "" {
		params.Symbol = symbol
	}
	if interval != "" {
		params.Interval = interval
	}
	params.Limit = limit

	var rows []DataRow
	var candlesErr error
	if rows = s.bot.RowsFor(params.Symbol, params.Interval); rows == nil {
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		candles, err := FetchCandles(ctx, params.Symbol, params.Interval, limit)
		if err != nil {
			candlesErr = err
		} else {
			rows = Analyze(candles, params)
		}
	}
	if candlesErr != nil {
		writeErr(w, http.StatusBadGateway, candlesErr.Error())
		return
	}

	type rowOut struct {
		T      string   `json:"t"`
		O      float64  `json:"o"`
		H      float64  `json:"h"`
		L      float64  `json:"l"`
		C      float64  `json:"c"`
		V      float64  `json:"v"`
		RSI    *float64 `json:"rsi"`
		StochK *float64 `json:"stochK"`
		StochD *float64 `json:"stochD"`
		HMA    *float64 `json:"hma"`
		Buy    int      `json:"buy"`
		Sell   int      `json:"sell"`
	}
	out := make([]rowOut, 0, len(rows))
	for _, r := range rows {
		out = append(out, rowOut{
			T: r.Time.Format("2006-01-02 15:04:05"),
			O: r.Open, H: r.High, L: r.Low, C: r.Close, V: r.Volume,
			RSI:    nanP(r.RSI),
			StochK: nanP(r.StochK),
			StochD: nanP(r.StochD),
			HMA:    nanP(r.HMA),
			Buy:    r.Buy,
			Sell:   r.Sell,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"symbol":      params.Symbol,
		"interval":    params.Interval,
		"strategy":    params.StrategyID,
		"rows":        out,
		"lastUpdated": nowStamp(),
	})
}

func nanP(v float64) *float64 {
	if v != v {
		return nil
	}
	return &v
}

func (s *Server) handleTrades(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.bot.Trades())
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.bot.Status())
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	var p Params
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&p)
	}
	if err := s.bot.Start(p); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.bot.Status())
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	s.bot.Stop()
	writeJSON(w, http.StatusOK, s.bot.Status())
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	files, err := s.bot.Export()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	n := 200
	writeJSON(w, http.StatusOK, map[string]any{
		"logs": s.bot.logbuf.Lines(n),
	})
}

func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/files/")
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join(s.bot.dataDir, name))
}

// handleStream streams SSE events to the browser.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, cancel := s.hub.Subscribe()
	defer cancel()

	// Send initial snapshot so a fresh page renders immediately.
	init := func() {
		fmt.Fprintf(w, "event: status\ndata: ")
		_ = json.NewEncoder(w).Encode(s.bot.Status())
		fmt.Fprintf(w, "\n\n")
		flusher.Flush()
	}
	init()

	keepAlive := time.NewTicker(15 * time.Second)
	defer keepAlive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case frame, ok := <-ch:
			if !ok {
				return
			}
			_, _ = w.Write(frame)
			flusher.Flush()
		case <-keepAlive.C:
			_, _ = w.Write([]byte(": keepalive\n\n"))
			flusher.Flush()
		}
	}
}
