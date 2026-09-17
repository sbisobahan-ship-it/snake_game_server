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
	m := &Manager{
		room:    room,
		clients: make(map[string]*Client),
	}

	// Register high-speed real-time eaten food broadcast callback:
	// Broadcasts eaten food ID(s) to ALL connected clients in real-time
	room.SetEatBatchCallback(func(events []game.FoodEatenEvent) {
		if len(events) == 0 {
			return
		}
		// 1. Binary broadcast (Opcode 0x06)
		binData := EncodeEatBatchBinary(events)
		m.BroadcastBinary(binData)

		// JSON broadcast omitted for pure binary high-speed stream

		monitor.DefaultHub.Emit(monitor.ChanFood, "eat", "⚡ [REAL-TIME EAT BROADCAST] Broadcast %d eaten food(s) to %d connected players", len(events), m.GetActiveClientCount())
	})

	m.StartStoreSyncTicker()

	return m
}

// HandleWS handles incoming WebSocket connections
func (m *Manager) HandleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("❌ WebSocket upgrade failed: %v", err)
		monitor.DefaultHub.Emit(monitor.ChanNetwork, "error", "❌ WebSocket upgrade failed: %v (IP: %s)", err, r.RemoteAddr)
		return
	}

	token := r.URL.Query().Get("token")
	clientID := r.URL.Query().Get("id")
	if clientID == "" {
		clientID = r.URL.Query().Get("player_id")
	}
	if clientID == "" {
		clientID = r.URL.Query().Get("client_id")
	}

	// 1. Check if reconnecting via token
	if token != "" {
		// A. Check if the snake is still alive in arena
		if p, isAlive := m.room.GetPlayerByToken(token); isAlive {
			clientID = p.ID
			client := NewClient(clientID, conn, m.handleMessage, m.handleDisconnect)
			client.Name = p.Name

			var oldClient *Client
			m.mu.Lock()
			if old, exists := m.clients[clientID]; exists {
				oldClient = old
			}
			m.clients[clientID] = client
			total := len(m.clients)
			m.mu.Unlock()

			if oldClient != nil {
				oldClient.Close()
			}

			log.Printf("🔄 [Client Reconnected by Token] ID: %s | Token: %s | Total: %d", clientID, token, total)
			monitor.DefaultHub.Emit(monitor.ChanNetwork, "success", "🔄 [WS RECONNECT] Player '%s' (Token: %s) reconnected from %s", clientID, token, r.RemoteAddr)

			// Send instant authoritative catch-up state packet
			if pSync, ok := m.room.GetPlayerSync(clientID); ok {
				client.SendJSON(map[string]interface{}{
					"type": OpReconnectSuccess,
					"payload": map[string]interface{}{
						"id":        pSync.ID,
						"token":     pSync.Token,
						"name":      pSync.Name,
						"spawn_x":   pSync.Head.X,
						"spawn_y":   pSync.Head.Y,
						"angle":     pSync.Angle,
						"score":     pSync.Score,
						"alive":     pSync.IsAlive,
						"boost":     pSync.IsBoost,
						"skin_id":   pSync.SkinID,
						"body":      pSync.Body,
						"world_w":   m.room.Config.WorldWidth,
						"world_h":   m.room.Config.WorldHeight,
						"tick_rate": m.room.Config.TickRate,
						"timestamp": time.Now().UnixMilli(),
					},
				})
				client.SendBinary(EncodeLocationSyncBinary(pSync))
			}

			go client.WritePump()
			go client.ReadPump()
			return
		}

		// B. Check if snake already died while offline
		if ds, isDead := m.room.GetDeadSession(token); isDead {
			log.Printf("💀 [Token Reconnect Rejected (Dead Snake)] Token: %s | ID: %s | Score: %d", token, ds.PlayerID, ds.FinalScore)
			monitor.DefaultHub.Emit(monitor.ChanPlayer, "warn", "💀 [TOKEN RECONNECT REJECTED] Snake died during offline period. ID: %s | Final Score: %d", ds.PlayerID, ds.FinalScore)

			conn.SetWriteDeadline(time.Now().Add(writeWait))
			_ = conn.WriteJSON(map[string]interface{}{
				"type": OpGameOver,
				"payload": GameOverPayload{
					ID:         ds.PlayerID,
					Token:      ds.Token,
					Reason:     ds.DeathReason,
					FinalScore: ds.FinalScore,
					DiedAt:     ds.DiedAt,
					Status:     "dead",
				},
			})
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "snake_dead"), time.Now().Add(writeWait))
			conn.Close()
			return
		}
	}

	if clientID == "" {
		clientID = fmt.Sprintf("p_%d", atomic.AddUint64(&m.clientSeq, 1))
	}

	client := NewClient(
		clientID,
		conn,
		m.handleMessage,
		m.handleDisconnect,
	)

	var oldClient *Client
	m.mu.Lock()
	if old, exists := m.clients[clientID]; exists {
		oldClient = old
	}
	m.clients[clientID] = client
	total := len(m.clients)
	m.mu.Unlock()

	if oldClient != nil {
		oldClient.Close()
	}

	log.Printf("📱 [Client Connected] ID: %s | Total: %d", clientID, total)
	monitor.DefaultHub.Emit(monitor.ChanNetwork, "success", "📱 [WS CONNECT] Game device '%s' connected from %s (Total Game Devices: %d)", clientID, r.RemoteAddr, total)

	// Send initial welcome packet
	welcomePayload := map[string]interface{}{
		"id":        clientID,
		"world_w":   m.room.Config.WorldWidth,
		"world_h":   m.room.Config.WorldHeight,
		"tick_rate": m.room.Config.TickRate,
		"timestamp": time.Now().UnixMilli(),
	}
	if pSync, ok := m.room.GetPlayerSync(clientID); ok {
		welcomePayload["reconnected"] = true
		welcomePayload["token"] = pSync.Token
		welcomePayload["player"] = map[string]interface{}{
			"x":     pSync.Head.X,
			"y":     pSync.Head.Y,
			"angle": pSync.Angle,
			"score": pSync.Score,
			"alive": pSync.IsAlive,
		}
	}
	client.SendJSON(map[string]interface{}{
		"type":    "welcome",
		"payload": welcomePayload,
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
			if pSync, ok := m.room.GetPlayerSync(client.ID); ok {
				client.SendBinary(EncodeLocationSyncBinary(pSync))
			}

		case BinOpPong: // 0x04: Binary Pong Keepalive
			// Client acknowledged ping

		case BinOpLocationSync: // 0x08: Client requests authoritative server location sync
			if pSync, ok := m.room.GetPlayerSync(client.ID); ok {
				client.SendBinary(EncodeLocationSyncBinary(pSync))
			}

		case BinOpLocation: // 0x07: Direct authoritative client location stream
			if x, y, angle, boost, ok := DecodeLocationBinary(raw); ok {
				m.room.UpdatePlayerLocation(client.ID, x, y, angle, boost)
				name := client.Name
				if name == "" {
					name = client.ID
				}
				monitor.DefaultHub.UpdateDeviceLocation(client.ID, name, x, y, angle)
				fmt.Printf("\r📍 [DEVICE: %-8s] X: %-8.1f | Y: %-8.1f | Angle: %-6.2f rad   ", name, x, y, angle)
			}

		default:
			// Discard any unauthorized or unrecognized binary opcode
			monitor.DefaultHub.Emit(monitor.ChanNetwork, "warn", "⚠️ [BINARY ROUTE REJECTED] Unrecognized or disallowed opcode 0x%02X from '%s'", raw[0], client.ID)
		}
		return
	}

	// 2. Text / JSON Packet Frame
	var base BaseMessage
	if err := json.Unmarshal(raw, &base); err != nil {
		return
	}

	// Auto-detect start calls if type was omitted but "start": true is present
	if base.Type == "" {
		var checkStart struct {
			Start bool `json:"start"`
		}
		if json.Unmarshal(raw, &checkStart) == nil && checkStart.Start {
			base.Type = OpStart
		}
	}

	switch base.Type {
	case OpReconnect:
		var rec ReconnectPayload
		if base.Payload != nil {
			if payloadBytes, err := json.Marshal(base.Payload); err == nil {
				_ = json.Unmarshal(payloadBytes, &rec)
			}
		}
		if rec.Token == "" {
			_ = json.Unmarshal(raw, &rec)
		}

		if rec.Token != "" {
			// A. Active living snake reconnect
			if p, isAlive := m.room.GetPlayerByToken(rec.Token); isAlive {
				client.ID = p.ID
				client.Name = p.Name
				var oldClient *Client
				m.mu.Lock()
				if old, exists := m.clients[client.ID]; exists && old != client {
					oldClient = old
				}
				m.clients[client.ID] = client
				m.mu.Unlock()

				if oldClient != nil {
					oldClient.Close()
				}

				if pSync, ok := m.room.GetPlayerSync(p.ID); ok {
					client.SendJSON(map[string]interface{}{
						"type": OpReconnectSuccess,
						"payload": map[string]interface{}{
							"id":        pSync.ID,
							"token":     pSync.Token,
							"name":      pSync.Name,
							"spawn_x":   pSync.Head.X,
							"spawn_y":   pSync.Head.Y,
							"angle":     pSync.Angle,
							"score":     pSync.Score,
							"alive":     pSync.IsAlive,
							"boost":     pSync.IsBoost,
							"skin_id":   pSync.SkinID,
							"body":      pSync.Body,
							"world_w":   m.room.Config.WorldWidth,
							"world_h":   m.room.Config.WorldHeight,
							"tick_rate": m.room.Config.TickRate,
							"timestamp": time.Now().UnixMilli(),
						},
					})
					client.SendBinary(EncodeLocationSyncBinary(pSync))
				}
				return
			}

			// B. Snake died while offline
			if ds, isDead := m.room.GetDeadSession(rec.Token); isDead {
				client.SendJSON(map[string]interface{}{
					"type": OpGameOver,
					"payload": GameOverPayload{
						ID:         ds.PlayerID,
						Token:      ds.Token,
						Reason:     ds.DeathReason,
						FinalScore: ds.FinalScore,
						DiedAt:     ds.DiedAt,
						Status:     "dead",
					},
				})
				return
			}
		}

		// Fallback: Check if client.ID is alive in room
		if pSync, ok := m.room.GetPlayerSync(client.ID); ok && pSync.IsAlive {
			client.SendJSON(map[string]interface{}{
				"type": OpReconnectSuccess,
				"payload": map[string]interface{}{
					"id":        pSync.ID,
					"token":     pSync.Token,
					"name":      pSync.Name,
					"spawn_x":   pSync.Head.X,
					"spawn_y":   pSync.Head.Y,
					"angle":     pSync.Angle,
					"score":     pSync.Score,
					"alive":     pSync.IsAlive,
					"boost":     pSync.IsBoost,
					"skin_id":   pSync.SkinID,
					"body":      pSync.Body,
					"world_w":   m.room.Config.WorldWidth,
					"world_h":   m.room.Config.WorldHeight,
					"tick_rate": m.room.Config.TickRate,
					"timestamp": time.Now().UnixMilli(),
				},
			})
			client.SendBinary(EncodeLocationSyncBinary(pSync))
			return
		}

		client.SendJSON(map[string]interface{}{
			"type": OpGameOver,
			"payload": map[string]interface{}{
				"id":     client.ID,
				"status": "dead",
				"reason": "session_expired_or_not_found",
			},
		})

	case OpJoin, OpStart, OpStartGame, "play", "game_start", "respawn", "restart", "play_again":
		var join JoinPayload
		// 1. Try unpacking from base.Payload first
		if base.Payload != nil {
			if payloadBytes, err := json.Marshal(base.Payload); err == nil {
				_ = json.Unmarshal(payloadBytes, &join)
			}
		}
		// 2. Fallback: if fields were sent at root level in JSON (e.g. {"type":"start", "start":true, "name":"..."})
		if join.Name == "" && join.PlayerID == "" && join.ID == "" {
			_ = json.Unmarshal(raw, &join)
		}

		targetID := client.ID
		if join.ID != "" {
			targetID = join.ID
		} else if join.PlayerID != "" {
			targetID = join.PlayerID
		}
		if join.Name == "" {
			join.Name = targetID
		}
		client.Name = join.Name
		p := m.room.AddPlayerWithToken(targetID, join.Token, join.Name, join.SkinID)
		log.Printf("🎮 Player Started / Joined: %s (Token: %s | Name: %s | Live: %.0f, %.0f)", targetID, p.Token, join.Name, p.Snake.Head.X, p.Snake.Head.Y)

		startPayload := map[string]interface{}{
			"id":        targetID,
			"token":     p.Token,
			"name":      p.Name,
			"skin_id":   p.Snake.SkinID,
			"spawn_x":   p.Snake.Head.X,
			"spawn_y":   p.Snake.Head.Y,
			"angle":     p.Snake.Angle,
			"score":     p.Snake.Score,
			"alive":     p.Snake.IsAlive,
			"start":     true,
			"world_w":   m.room.Config.WorldWidth,
			"world_h":   m.room.Config.WorldHeight,
			"tick_rate": m.room.Config.TickRate,
			"timestamp": time.Now().UnixMilli(),
		}

		// Send started confirmation packet (and joined for compatibility)
		client.SendJSON(map[string]interface{}{
			"type":    OpStarted,
			"payload": startPayload,
		})
		client.SendJSON(map[string]interface{}{
			"type":    OpJoined,
			"payload": startPayload,
		})

		// Send instant authoritative location sync so client immediately snaps to accurate server location
		if pSync, ok := m.room.GetPlayerSync(targetID); ok {
			client.SendJSON(map[string]interface{}{
				"type": OpLocationSync,
				"payload": map[string]interface{}{
					"id":        pSync.ID,
					"token":     pSync.Token,
					"name":      pSync.Name,
					"x":         pSync.Head.X,
					"y":         pSync.Head.Y,
					"angle":     pSync.Angle,
					"score":     pSync.Score,
					"alive":     pSync.IsAlive,
					"boost":     pSync.IsBoost,
					"skin_id":   pSync.SkinID,
					"body":      pSync.Body,
					"timestamp": time.Now().UnixMilli(),
				},
			})
			client.SendBinary(EncodeLocationSyncBinary(pSync))
		}

	case OpSync, OpLocationSync, "get_location", "get_state":
		if pSync, ok := m.room.GetPlayerSync(client.ID); ok {
			client.SendJSON(map[string]interface{}{
				"type": OpLocationSync,
				"payload": map[string]interface{}{
					"id":        pSync.ID,
					"token":     pSync.Token,
					"name":      pSync.Name,
					"x":         pSync.Head.X,
					"y":         pSync.Head.Y,
					"angle":     pSync.Angle,
					"score":     pSync.Score,
					"alive":     pSync.IsAlive,
					"boost":     pSync.IsBoost,
					"skin_id":   pSync.SkinID,
					"body":      pSync.Body,
					"timestamp": time.Now().UnixMilli(),
				},
			})
		}

	case OpPlayerDie, "die", "defeat", "dead", "leave", "default", "reset":
		m.room.RemovePlayer(client.ID)
		log.Printf("💀 Player Left / Reset: %s (Type: %s)", client.ID, base.Type)
		client.SendJSON(map[string]interface{}{
			"type": "player_die_ack",
			"payload": map[string]interface{}{
				"id":     client.ID,
				"status": "reset",
			},
		})

	case OpInput:
		var in InputPayload
		if payloadBytes, err := json.Marshal(base.Payload); err == nil {
			if json.Unmarshal(payloadBytes, &in) == nil {
				m.room.UpdatePlayerInput(client.ID, in.TargetAngle, in.IsBoosting)
			}
		}

	case OpLocation:
		var loc LocationPayload
		if payloadBytes, err := json.Marshal(base.Payload); err == nil {
			if json.Unmarshal(payloadBytes, &loc) == nil {
				m.room.UpdatePlayerLocation(client.ID, loc.X, loc.Y, loc.Angle, loc.IsBoosting)
				name := client.Name
				if name == "" {
					name = client.ID
				}
				monitor.DefaultHub.UpdateDeviceLocation(client.ID, name, loc.X, loc.Y, loc.Angle)
				fmt.Printf("\r📍 [DEVICE: %-8s] X: %-8.1f | Y: %-8.1f | Angle: %-6.2f rad   ", name, loc.X, loc.Y, loc.Angle)
			}
		}

	case OpFoodRelay: // "food": Relay JSON Food payload directly to all other clients
		m.BroadcastJSONExcept(client.ID, OpFoodRelay, base.Payload)
		monitor.DefaultHub.Emit(monitor.ChanFood, "spawn", "🍎 [JSON FOOD RELAY] Broadcast food payload from '%s'", client.ID)

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
		resp := map[string]interface{}{
			"type": OpPong,
			"payload": map[string]interface{}{
				"ts": time.Now().UnixMilli(),
			},
		}
		if pSync, ok := m.room.GetPlayerSync(client.ID); ok {
			resp["player"] = map[string]interface{}{
				"x":     pSync.Head.X,
				"y":     pSync.Head.Y,
				"angle": pSync.Angle,
				"score": pSync.Score,
				"alive": pSync.IsAlive,
			}
		}
		client.SendJSON(resp)

	default:
		// Discard unrecognized or disabled JSON message types
		monitor.DefaultHub.Emit(monitor.ChanNetwork, "warn", "⚠️ [JSON ROUTE REJECTED] Disallowed or unrecognized packet type '%s' from '%s'", base.Type, client.ID)
	}
}

// handleDisconnect removes socket connection but preserves player entity for smooth reconnect
func (m *Manager) handleDisconnect(client *Client) {
	m.mu.Lock()
	isCurrent := false
	if current, exists := m.clients[client.ID]; exists && current == client {
		delete(m.clients, client.ID)
		isCurrent = true
	}
	total := len(m.clients)
	m.mu.Unlock()

	if isCurrent {
		monitor.DefaultHub.RemoveDevice(client.ID)
		m.room.MarkPlayerDisconnected(client.ID)
		log.Printf("🔌 [Client Disconnected (Preserved)] ID: %s | Remaining Game Sockets: %d", client.ID, total)
		monitor.DefaultHub.Emit(monitor.ChanNetwork, "warn", "🔌 [WS DISCONNECT] Game device '%s' disconnected (Remaining Game Sockets: %d)", client.ID, total)
	}
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

// StartStoreSyncTicker starts the periodic 30-second store sync loop for all connected clients
func (m *Manager) StartStoreSyncTicker() {
	ticker := time.NewTicker(30 * time.Second)
	go func() {
		for range ticker.C {
			m.broadcastStoreSync()
		}
	}()
}

// broadcastStoreSync pushes current authoritative score, segment count, and food statistics to all connected clients every 30s
func (m *Manager) broadcastStoreSync() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.clients) == 0 {
		return
	}

	now := time.Now().UnixMilli()
	for _, client := range m.clients {
		if pSync, ok := m.room.GetPlayerSync(client.ID); ok {
			segCount := len(pSync.Body)
			// 1. Send dedicated StoreSync JSON packet
			client.SendJSON(map[string]interface{}{
				"type": OpStoreSync,
				"payload": map[string]interface{}{
					"id":                 pSync.ID,
					"name":               pSync.Name,
					"score":              pSync.Score,
					"segments":           segCount,
					"static_foods_eaten": pSync.StaticFoodsEaten,
					"ts":                 now,
				},
			})

			// 2. Also send binary location sync so binary state receivers get score & segments
			client.SendBinary(EncodeLocationSyncBinary(pSync))

			monitor.DefaultHub.Emit(monitor.ChanPlayer, "info", "⏱️ [30S STORE SYNC] Client '%s' -> Score: %d | Segments: %d | Foods: %d", client.ID, pSync.Score, segCount, pSync.StaticFoodsEaten)
		}
	}
}
