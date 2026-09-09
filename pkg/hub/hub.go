package hub

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"snake_game_server/pkg/game"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 1024
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all CORS connections
	},
}

type InMessage struct {
	Type      string         `json:"type"`
	Name      string         `json:"name,omitempty"`
	Direction game.Direction `json:"direction,omitempty"`
}

type OutMessage struct {
	Type      string         `json:"type"`
	PlayerID  string         `json:"playerId,omitempty"`
	State     game.GameState `json:"state,omitempty"`
	Message   string         `json:"message,omitempty"`
	Connected int            `json:"connected,omitempty"`
}

type Client struct {
	Hub      *Hub
	Conn     *websocket.Conn
	Send     chan []byte
	PlayerID string
	Name     string
}

func (c *Client) ReadPump() {
	defer func() {
		c.Hub.Unregister <- c
		c.Conn.Close()
	}()

	c.Conn.SetReadLimit(maxMessageSize)
	c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("ws read error: %v", err)
			}
			break
		}

		var in InMessage
		if err := json.Unmarshal(message, &in); err != nil {
			continue
		}

		switch in.Type {
		case "join":
			if in.Name != "" {
				c.Name = in.Name
			}
			c.Hub.Game.AddPlayer(c.PlayerID, c.Name)
		case "direction":
			c.Hub.Game.SetDirection(c.PlayerID, in.Direction)
		case "respawn":
			c.Hub.Game.RespawnPlayer(c.PlayerID)
		}
	}
}

func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Hub closed the channel
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.Conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued chat messages to the current websocket message
			n := len(c.Send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-c.Send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

type Hub struct {
	mu         sync.RWMutex
	Clients    map[*Client]bool
	Broadcast  chan []byte
	Register   chan *Client
	Unregister chan *Client
	Game       *game.GameEngine
}

func NewHub(gameEngine *game.GameEngine) *Hub {
	h := &Hub{
		Clients:    make(map[*Client]bool),
		Broadcast:  make(chan []byte, 256),
		Register:   make(chan *Client),
		Unregister: make(chan *Client),
		Game:       gameEngine,
	}

	gameEngine.OnStateTick = func(state game.GameState) {
		payload, err := json.Marshal(OutMessage{
			Type:      "state",
			State:     state,
			Connected: h.PlayerCount(),
		})
		if err == nil {
			h.Broadcast <- payload
		}
	}

	return h
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.Register:
			h.mu.Lock()
			h.Clients[client] = true
			h.mu.Unlock()

			// Send init message with player ID
			initMsg, _ := json.Marshal(OutMessage{
				Type:     "init",
				PlayerID: client.PlayerID,
				Message:  "Connected to Snake WebSocket Server",
			})
			client.Send <- initMsg

		case client := <-h.Unregister:
			h.mu.Lock()
			if _, ok := h.Clients[client]; ok {
				delete(h.Clients, client)
				close(client.Send)
				h.Game.RemovePlayer(client.PlayerID)
			}
			h.mu.Unlock()

		case message := <-h.Broadcast:
			h.mu.RLock()
			for client := range h.Clients {
				select {
				case client.Send <- message:
				default:
					close(client.Send)
					delete(h.Clients, client)
					h.Game.RemovePlayer(client.PlayerID)
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *Hub) PlayerCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.Clients)
}

func (h *Hub) ServeWs(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("failed to upgrade to websocket: %v", err)
		return
	}

	playerID := r.URL.Query().Get("id")
	if playerID == "" {
		playerID = randomID()
	}

	name := r.URL.Query().Get("name")
	if name == "" {
		name = "Player-" + playerID[:4]
	}

	client := &Client{
		Hub:      h,
		Conn:     conn,
		Send:     make(chan []byte, 256),
		PlayerID: playerID,
		Name:     name,
	}

	h.Register <- client

	go client.WritePump()
	go client.ReadPump()
}

func randomID() string {
	chars := "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 8)
	for i := range b {
		b[i] = chars[time.Now().UnixNano()%int64(len(chars))]
		time.Sleep(1 * time.Microsecond)
	}
	return string(b)
}
