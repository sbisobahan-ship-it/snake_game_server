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
	ChanFood      EventType = "food"
	ChanPlayer    EventType = "player"
	ChanNetwork   EventType = "network"
	ChanPhysics   EventType = "physics"
	ChanMetrics   EventType = "metrics"
	ChanLocations EventType = "locations"
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

// DeviceLocationState tracks real-time state for each individual connected client device
type DeviceLocationState struct {
	ID          string  `json:"id"`
	Name        string  `json:"name,omitempty"`
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
	Angle       float64 `json:"angle"`
	AngleDeg    float64 `json:"angle_deg"`
	FPS         float64 `json:"fps"`
	PacketCount uint64  `json:"packets"`
	LastSeenMs  int64   `json:"last_seen_ms"`

	lastPktTime time.Time
	fpsCounter  int
	fpsWindow   time.Time
}

// ServerMetrics holds live telemetry statistics
type ServerMetrics struct {
	ActivePlayers int                   `json:"active_players"`  // Pure Game Players / Devices on /ws
	AdminMonitors int                   `json:"admin_monitors"`  // Dedicated Admin Dashboard Sessions on /ws/monitor
	TotalFoods    int                   `json:"total_foods"`
	TickRate      int                   `json:"tick_rate"`
	UptimeSec     int64                 `json:"uptime_sec"`
	AllocMemMB    float64               `json:"alloc_mem_mb"`
	NumGoroutine  int                   `json:"goroutines"`
	TotalEaten    uint64                `json:"total_eaten"`
	PacketsIn     uint64                `json:"packets_in"`
	PacketsOut    uint64                `json:"packets_out"`
	LiveX         float64               `json:"live_x"`
	LiveY         float64               `json:"live_y"`
	LiveAngle     float64               `json:"live_angle"`
	HasLocation   bool                  `json:"has_location"`
	Devices       []DeviceLocationState `json:"devices,omitempty"`
}

// Hub manages subscriber channels
type Hub struct {
	mu              sync.RWMutex
	subscribers     map[chan LogEvent]struct{}
	startTime       time.Time
	metrics         ServerMetrics
	deviceLocations map[string]*DeviceLocationState
}

var DefaultHub = NewHub()

// NewHub creates a new monitor hub
func NewHub() *Hub {
	h := &Hub{
		subscribers:     make(map[chan LogEvent]struct{}),
		startTime:       time.Now(),
		deviceLocations: make(map[string]*DeviceLocationState),
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

// UpdateDeviceLocation updates real-time coordinates, angle, packet counts, and calculates live FPS per device
func (h *Hub) UpdateDeviceLocation(playerID, name string, x, y, angle float64) {
	now := time.Now()
	h.mu.Lock()
	dev, exists := h.deviceLocations[playerID]
	if !exists {
		dev = &DeviceLocationState{
			ID:          playerID,
			Name:        name,
			lastPktTime: now,
			fpsWindow:   now,
		}
		h.deviceLocations[playerID] = dev
	}
	if name != "" {
		dev.Name = name
	}
	dev.X = x
	dev.Y = y
	dev.Angle = angle
	dev.AngleDeg = angle * (180.0 / 3.141592653589793)
	dev.PacketCount++
	dev.LastSeenMs = now.UnixMilli()
	dev.fpsCounter++

	// Recalculate stream FPS dynamically every 500ms window
	if now.Sub(dev.fpsWindow) >= 500*time.Millisecond {
		durationSec := now.Sub(dev.fpsWindow).Seconds()
		if durationSec > 0 {
			dev.FPS = float64(dev.fpsCounter) / durationSec
		}
		dev.fpsCounter = 0
		dev.fpsWindow = now
	}
	dev.lastPktTime = now

	devSnapshot := *dev

	// Also update global top-level metrics
	h.metrics.LiveX = x
	h.metrics.LiveY = y
	h.metrics.LiveAngle = angle
	h.metrics.HasLocation = true
	h.mu.Unlock()

	// Broadcast instantaneous 0-delay location update to all active dashboard subscribers
	h.mu.RLock()
	defer h.mu.RUnlock()
	if len(h.subscribers) > 0 {
		event := LogEvent{
			Channel:   ChanLocations,
			Level:     "live",
			Data:      devSnapshot,
			Timestamp: now.Format("15:04:05.000"),
			UnixMs:    now.UnixMilli(),
		}
		for ch := range h.subscribers {
			select {
			case ch <- event:
			default:
			}
		}
	}
}

// RemoveDevice unregisters a device when it leaves or disconnects
func (h *Hub) RemoveDevice(playerID string) {
	h.mu.Lock()
	delete(h.deviceLocations, playerID)
	h.mu.Unlock()

	now := time.Now()
	h.mu.RLock()
	defer h.mu.RUnlock()
	if len(h.subscribers) > 0 {
		event := LogEvent{
			Channel:   ChanLocations,
			Level:     "remove",
			Data:      map[string]string{"id": playerID},
			Timestamp: now.Format("15:04:05.000"),
			UnixMs:    now.UnixMilli(),
		}
		for ch := range h.subscribers {
			select {
			case ch <- event:
			default:
			}
		}
	}
}

// GetDeviceLocations returns a snapshot of all active device location states
func (h *Hub) GetDeviceLocations() []DeviceLocationState {
	h.mu.RLock()
	defer h.mu.RUnlock()
	list := make([]DeviceLocationState, 0, len(h.deviceLocations))
	for _, d := range h.deviceLocations {
		list = append(list, *d)
	}
	return list
}

// UpdateLiveLocation legacy wrapper
func (h *Hub) UpdateLiveLocation(x, y, angle float64) {
	h.UpdateDeviceLocation("default", "Player", x, y, angle)
}

// EmitMetrics emits real-time server telemetry stats
func (h *Hub) EmitMetrics(activeGamePlayers, totalFoods, tickRate int, totalEaten uint64) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	now := time.Now()

	h.mu.RLock()
	adminCount := len(h.subscribers)
	liveX := h.metrics.LiveX
	liveY := h.metrics.LiveY
	liveAngle := h.metrics.LiveAngle
	hasLoc := h.metrics.HasLocation

	devices := make([]DeviceLocationState, 0, len(h.deviceLocations))
	for _, dev := range h.deviceLocations {
		devices = append(devices, *dev)
	}
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
		LiveX:         liveX,
		LiveY:         liveY,
		LiveAngle:     liveAngle,
		HasLocation:   hasLoc,
		Devices:       devices,
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
