package fxbot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Trade struct {
	ID           string     `json:"id"`
	Symbol       string     `json:"symbol"`
	Interval     string     `json:"interval"`
	Strategy     string     `json:"strategy"`
	Mode         string     `json:"mode"`
	Lots         float64    `json:"lots"`
	BuyDate      time.Time  `json:"buyDate"`
	BuyPrice     float64    `json:"buyPrice"`
	SellDate     *time.Time `json:"sellDate,omitempty"`
	SellPrice    float64    `json:"sellPrice,omitempty"`
	ProfitPoints float64    `json:"profitPoints,omitempty"`
	ProfitPct    float64    `json:"profitPct,omitempty"`
	Ticket       string     `json:"ticket,omitempty"`
	Status       string     `json:"status"` // open | closed
}

type TradeStore struct {
	mu    sync.Mutex
	path  string
	items []Trade
}

func LoadTradeStore(path string) *TradeStore {
	ts := &TradeStore{path: path}
	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, &ts.items)
	}
	return ts
}

func (s *TradeStore) List() []Trade {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Trade, len(s.items))
	copy(out, s.items)
	return out
}

func (s *TradeStore) Add(t Trade) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = append(s.items, t)
	s.saveLocked()
}

func (s *TradeStore) OpenTrade() *Trade {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.items) - 1; i >= 0; i-- {
		if s.items[i].Status == "open" {
			t := s.items[i]
			return &t
		}
	}
	return nil
}

func (s *TradeStore) Close(id string, sellPrice float64, ticket string, sellDate time.Time) *Trade {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.items) - 1; i >= 0; i-- {
		if s.items[i].ID == id && s.items[i].Status == "open" {
			s.items[i].Status = "closed"
			s.items[i].SellDate = &sellDate
			s.items[i].SellPrice = sellPrice
			s.items[i].ProfitPoints = sellPrice - s.items[i].BuyPrice
			if s.items[i].BuyPrice != 0 {
				s.items[i].ProfitPct = (sellPrice - s.items[i].BuyPrice) / s.items[i].BuyPrice * 100
			}
			s.items[i].Ticket = ticket
			s.saveLocked()
			t := s.items[i]
			return &t
		}
	}
	return nil
}

func (s *TradeStore) saveLocked() {
	if s.path == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(s.path), 0o755)
	data, err := json.MarshalIndent(s.items, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(s.path, data, 0o644)
}

func newTradeID(symbol string) string {
	return fmt.Sprintf("%d-%s", time.Now().UnixNano(), symbol)
}
