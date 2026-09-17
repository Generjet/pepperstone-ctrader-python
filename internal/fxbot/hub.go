package fxbot

import (
	"encoding/json"
	"sync"
	"time"
)

func nowStamp() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

// Hub fans out SSE events to connected browser clients.
type Hub struct {
	mu   sync.Mutex
	subs map[chan []byte]struct{}
}

func NewHub() *Hub {
	return &Hub{subs: make(map[chan []byte]struct{})}
}

func (h *Hub) Subscribe() (chan []byte, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan []byte, 64)
	h.subs[ch] = struct{}{}
	cancel := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
	}
	return ch, cancel
}

// Publish sends an event frame to every subscriber, dropping slow clients.
func (h *Hub) Publish(event string, v any) {
	payload, err := json.Marshal(v)
	if err != nil {
		return
	}
	frame := append([]byte("event: "+event+"\ndata: "), payload...)
	frame = append(frame, '\n', '\n')
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- frame:
		default:
		}
	}
}

func (h *Hub) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

type LogLine struct {
	At  string `json:"at"`
	Msg string `json:"msg"`
}

// LogBuf keeps a bounded history of bot log lines.
type LogBuf struct {
	mu    sync.Mutex
	lines []LogLine
	cap   int
}

func NewLogBuf(capacity int) *LogBuf {
	return &LogBuf{cap: capacity}
}

func (l *LogBuf) Add(msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, LogLine{At: nowStamp(), Msg: msg})
	if len(l.lines) > l.cap {
		l.lines = l.lines[len(l.lines)-l.cap:]
	}
}

func (l *LogBuf) Lines(n int) []LogLine {
	l.mu.Lock()
	defer l.mu.Unlock()
	if n <= 0 || n > len(l.lines) {
		n = len(l.lines)
	}
	start := len(l.lines) - n
	out := make([]LogLine, n)
	copy(out, l.lines[start:])
	return out
}
