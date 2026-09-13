package monitor

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// MonitorAuthToken is the required secret token for dashboard telemetry connection
const MonitorAuthToken = "12345678"

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024 * 32,
	WriteBufferSize: 1024 * 32,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// EventType represents the category of the log event
type EventType string

const (
	ChanFood    EventType = "food"
	ChanPlayer  EventType = "player"
	ChanNetwork EventType = "network"
	ChanPhysics EventType = "physics"
	ChanMetrics EventType = "metrics"
)

// LogEvent is a single structured real-time log entry
type LogEvent struct {
	Channel   EventType `json:"channel"`
	Level     string    `json:"level"` // "info", "success", "warn", "error", "eat", "spawn"
	Message   string    `json:"msg"`
	Data      any       `json:"data,omitempty"`
	Timestamp string    `json:"time"`
	UnixMs    int64     `json:"ts"`
}

// ServerMetrics holds live telemetry statistics
type ServerMetrics struct {
	ActivePlayers int     `json:"active_players"`  // Pure Game Players / Devices on /ws
	AdminMonitors int     `json:"admin_monitors"`  // Dedicated Admin Dashboard Sessions on /ws/monitor
	TotalFoods    int     `json:"total_foods"`
	TickRate      int     `json:"tick_rate"`
	UptimeSec     int64   `json:"uptime_sec"`
	AllocMemMB    float64 `json:"alloc_mem_mb"`
	NumGoroutine  int     `json:"goroutines"`
	TotalEaten    uint64  `json:"total_eaten"`
	PacketsIn     uint64  `json:"packets_in"`
	PacketsOut    uint64  `json:"packets_out"`
}

// Hub manages subscriber channels
type Hub struct {
	mu          sync.RWMutex
	subscribers map[chan LogEvent]struct{}
	startTime   time.Time
	metrics     ServerMetrics
}

var DefaultHub = NewHub()

// NewHub creates a new monitor hub
func NewHub() *Hub {
	h := &Hub{
		subscribers: make(map[chan LogEvent]struct{}),
		startTime:   time.Now(),
	}
	return h
}

// Subscribe registers a new listener channel
func (h *Hub) Subscribe() chan LogEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan LogEvent, 256)
	h.subscribers[ch] = struct{}{}
	return ch
}

// Unsubscribe removes a listener channel
func (h *Hub) Unsubscribe(ch chan LogEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.subscribers, ch)
	close(ch)
}

// GetSubscriberCount returns the number of active admin monitor connections
func (h *Hub) GetSubscriberCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subscribers)
}

// Emit broadcasts a log event to all dashboard subscribers
func (h *Hub) Emit(channel EventType, level string, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	now := time.Now()

	event := LogEvent{
		Channel:   channel,
		Level:     level,
		Message:   msg,
		Timestamp: now.Format("15:04:05.000"),
		UnixMs:    now.UnixMilli(),
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for ch := range h.subscribers {
		select {
		case ch <- event:
		default:
			// Drop if consumer is slow
		}
	}
}

// EmitMetrics emits real-time server telemetry stats
func (h *Hub) EmitMetrics(activeGamePlayers, totalFoods, tickRate int, totalEaten uint64) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	now := time.Now()

	h.mu.RLock()
	adminCount := len(h.subscribers)
	h.mu.RUnlock()

	metrics := ServerMetrics{
		ActivePlayers: activeGamePlayers,
		AdminMonitors: adminCount,
		TotalFoods:    totalFoods,
		TickRate:      tickRate,
		UptimeSec:     int64(now.Sub(h.startTime).Seconds()),
		AllocMemMB:    float64(m.Alloc) / (1024 * 1024),
		NumGoroutine:  runtime.NumGoroutine(),
		TotalEaten:    totalEaten,
	}

	event := LogEvent{
		Channel:   ChanMetrics,
		Level:     "info",
		Data:      metrics,
		Timestamp: now.Format("15:04:05"),
		UnixMs:    now.UnixMilli(),
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for ch := range h.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}

// HandleMonitorWS upgrades and streams telemetry logs via authenticated WebSocket
func (h *Hub) HandleMonitorWS(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token != MonitorAuthToken {
		h.Emit(ChanNetwork, "error", "⛔ [AUTH REJECTED] Unauthorized monitor connection attempt with invalid token from IP: %s", r.RemoteAddr)
		http.Error(w, "Unauthorized: Invalid Monitor Token. Provided token rejected.", http.StatusUnauthorized)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		h.Emit(ChanNetwork, "error", "⚠️ [MONITOR WS ERROR] Upgrade failed: %v (IP: %s)", err, r.RemoteAddr)
		return
	}
	defer conn.Close()

	ch := h.Subscribe()
	defer func() {
		h.Unsubscribe(ch)
		h.Emit(ChanNetwork, "warn", "🔌 [ADMIN MONITOR DISCONNECTED] Admin dashboard disconnected (IP: %s)", r.RemoteAddr)
	}()

	h.Emit(ChanNetwork, "success", "🔐 [ADMIN MONITOR CONNECTED] Admin dashboard authenticated via token '12345678' (IP: %s)", r.RemoteAddr)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				break
			}
			var action map[string]interface{}
			if err := json.Unmarshal(msg, &action); err == nil {
				if t, ok := action["type"].(string); ok && t == "ping" {
					conn.WriteJSON(map[string]interface{}{"type": "pong", "ts": time.Now().UnixMilli()})
				}
			}
		}
	}()

	for {
		select {
		case <-done:
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			if err := conn.WriteJSON(event); err != nil {
				return
			}
		}
	}
}
