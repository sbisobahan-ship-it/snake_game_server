package network

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"snake_game_server/internal/game"
	"snake_game_server/internal/monitor"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024 * 64,
	WriteBufferSize: 1024 * 64,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins
	},
}

// Manager manages all active WebSocket client connections
type Manager struct {
	room      *game.Room
	clients   map[string]*Client
	mu        sync.RWMutex
	clientSeq uint64
}

// NewManager creates a new network manager
func NewManager(room *game.Room) *Manager {
	return &Manager{
		room:    room,
		clients: make(map[string]*Client),
	}
}

// HandleWS handles incoming WebSocket connections
func (m *Manager) HandleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("❌ WebSocket upgrade failed: %v", err)
		monitor.DefaultHub.Emit(monitor.ChanNetwork, "error", "❌ WebSocket upgrade failed: %v (IP: %s)", err, r.RemoteAddr)
		return
	}

	clientID := fmt.Sprintf("p_%d", atomic.AddUint64(&m.clientSeq, 1))

	client := NewClient(
		clientID,
		conn,
		m.handleMessage,
		m.handleDisconnect,
	)

	m.mu.Lock()
	m.clients[clientID] = client
	total := len(m.clients)
	m.mu.Unlock()

	log.Printf("📱 [Client Connected] ID: %s | Total: %d", clientID, total)
	monitor.DefaultHub.Emit(monitor.ChanNetwork, "success", "📱 [WS CONNECT] Game device '%s' connected from %s (Total Game Devices: %d)", clientID, r.RemoteAddr, total)

	// Send initial welcome packet
	client.SendJSON(map[string]interface{}{
		"type": "welcome",
		"payload": map[string]interface{}{
			"id":        clientID,
			"world_w":   m.room.Config.WorldWidth,
			"world_h":   m.room.Config.WorldHeight,
			"tick_rate": m.room.Config.TickRate,
			"timestamp": time.Now().UnixMilli(),
		},
	})

	go client.WritePump()
	go client.ReadPump()
}

// handleMessage parses incoming messages (Text JSON or Raw Binary Block)
func (m *Manager) handleMessage(client *Client, msgType int, raw []byte) {
	// 1. High-Speed Binary Packet Frame
	if msgType == websocket.BinaryMessage {
		if len(raw) == 0 {
			return
		}
		switch raw[0] {
		case BinOpInput: // Steering input (6 bytes)
			if angle, boost, ok := DecodeInputBinary(raw); ok {
				m.room.UpdatePlayerInput(client.ID, angle, boost)
			}

		case BinOpFoodRelay: // 0x05: Pure Binary Food Relay
			m.BroadcastBinaryExcept(client.ID, raw)
			monitor.DefaultHub.Emit(monitor.ChanFood, "spawn", "🍎 [BINARY FOOD RELAY] Broadcast %d bytes food payload from '%s'", len(raw), client.ID)

		case BinOpPing: // 0x03: Binary Ping Keepalive
			client.SendBinary([]byte{BinOpPong})

		case BinOpPong: // 0x04: Binary Pong Keepalive
			// Client acknowledged ping

		case BinOpEatBatch: // 0x06: Binary Eat Food Action
			if items, ok := DecodeEatFoodBinary(raw); ok {
				for _, item := range items {
					// Print clearly to server terminal console
					log.Printf("🍎 [FOOD EATEN] Client: %s | Food ID: #%d | Score Gain: +%d | Raw Hex: %X", client.ID, item.FoodID, item.ScoreGain, raw)
					m.room.ClaimAndEatFood(client.ID, item.FoodID, item.ScoreGain)
					monitor.DefaultHub.Emit(monitor.ChanFood, "eat", "🍎 [FOOD EATEN] '%s' ate Food #%d (+%d pts)", client.ID, item.FoodID, item.ScoreGain)
				}
			} else {
				log.Printf("⚠️ [FOOD EAT ERROR] Invalid binary payload from %s: %X", client.ID, raw)
			}
		}
		return
	}

	// 2. Text / JSON Packet Frame
	var base BaseMessage
	if err := json.Unmarshal(raw, &base); err != nil {
		return
	}

	switch base.Type {
	case OpJoin:
		var join JoinPayload
		if payloadBytes, err := json.Marshal(base.Payload); err == nil {
			if json.Unmarshal(payloadBytes, &join) == nil {
				if join.Name == "" {
					join.Name = client.ID
				}
				m.room.AddPlayer(client.ID, join.Name, join.SkinID)
				log.Printf("🎮 Player Joined: %s (Name: %s)", client.ID, join.Name)
			}
		}

	case OpInput:
		var in InputPayload
		if payloadBytes, err := json.Marshal(base.Payload); err == nil {
			if json.Unmarshal(payloadBytes, &in) == nil {
				m.room.UpdatePlayerInput(client.ID, in.TargetAngle, in.IsBoosting)
			}
		}

	case OpFoodRelay: // "food": Relay JSON Food payload directly to all other clients
		m.BroadcastJSONExcept(client.ID, OpFoodRelay, base.Payload)
		monitor.DefaultHub.Emit(monitor.ChanFood, "spawn", "🍎 [JSON FOOD RELAY] Broadcast food payload from '%s'", client.ID)

	case OpEatFood:
		var eat EatFoodPayload
		if payloadBytes, err := json.Marshal(base.Payload); err == nil {
			if json.Unmarshal(payloadBytes, &eat) == nil {
				gain := eat.Value
				if gain <= 0 {
					gain = 1
				}
				accepted, newScore := m.room.ClaimAndEatFood(client.ID, eat.FoodID, gain)
				if accepted {
					monitor.DefaultHub.Emit(monitor.ChanFood, "eat", "🍎 [JSON EAT ACCEPTED] '%s' claimed Food #%d (+%d pts -> Score: %d)", client.ID, eat.FoodID, gain, newScore)
				} else {
					monitor.DefaultHub.Emit(monitor.ChanFood, "warn", "⚠️ [JSON EAT REJECTED] '%s' tried Food #%d (Already claimed)", client.ID, eat.FoodID)
				}
			}
		}

	case OpChat:
		var chat ChatPayload
		if payloadBytes, err := json.Marshal(base.Payload); err == nil {
			if json.Unmarshal(payloadBytes, &chat) == nil {
				if chat.Name == "" {
					chat.Name = client.ID
				}
				chat.SenderID = client.ID
				chat.Timestamp = time.Now().UnixMilli()
				m.BroadcastJSON(OpChat, chat)
				monitor.DefaultHub.Emit(monitor.ChanPlayer, "info", "💬 [CHAT] '%s': %s", chat.Name, chat.Message)
			}
		}

	case OpPing:
		client.SendJSON(map[string]interface{}{
			"type": OpPong,
			"payload": map[string]interface{}{
				"ts": time.Now().UnixMilli(),
			},
		})
	}
}

// handleDisconnect removes player on connection drop
func (m *Manager) handleDisconnect(client *Client) {
	m.mu.Lock()
	delete(m.clients, client.ID)
	total := len(m.clients)
	m.mu.Unlock()

	m.room.RemovePlayer(client.ID)
	log.Printf("🔌 [Client Disconnected] ID: %s | Remaining: %d", client.ID, total)
	monitor.DefaultHub.Emit(monitor.ChanNetwork, "warn", "🔌 [WS DISCONNECT] Game device '%s' disconnected (Remaining Game Devices: %d)", client.ID, total)
}

// BroadcastWorldState sends unified binary block frame to all clients at 30 FPS
func (m *Manager) BroadcastWorldState(state *game.WorldState) {
	if len(state.Players) == 0 {
		return
	}

	binData := EncodeWorldStateBinary(state)

	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, client := range m.clients {
		client.SendBinary(binData)
	}
}

// BroadcastBinary sends raw binary payload to all clients
func (m *Manager) BroadcastBinary(data []byte) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, client := range m.clients {
		client.SendBinary(data)
	}
}

// BroadcastBinaryExcept sends raw binary payload to all clients EXCEPT skipID
func (m *Manager) BroadcastBinaryExcept(skipID string, data []byte) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for id, client := range m.clients {
		if id == skipID {
			continue
		}
		client.SendBinary(data)
	}
}

// BroadcastJSON sends JSON message to all clients
func (m *Manager) BroadcastJSON(msgType string, payload interface{}) {
	msg := BaseMessage{
		Type:    msgType,
		Payload: payload,
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, client := range m.clients {
		client.SendJSON(msg)
	}
}

// BroadcastJSONExcept sends JSON message to all clients EXCEPT skipID
func (m *Manager) BroadcastJSONExcept(skipID string, msgType string, payload interface{}) {
	msg := BaseMessage{
		Type:    msgType,
		Payload: payload,
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	for id, client := range m.clients {
		if id == skipID {
			continue
		}
		client.SendJSON(msg)
	}
}

// GetActiveClientCount returns count of connected game WebSockets
func (m *Manager) GetActiveClientCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.clients)
}
