package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Upgrader configures WebSocket connection upgrade
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all CORS connections
	},
}

// Client represents a connected WebSocket client
type Client struct {
	ID   string
	Conn *websocket.Conn
	Send chan []byte
}

// Hub manages active WebSocket clients and message broadcasting
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			log.Printf("[WebSocket] Client connected: %s (Total: %d)", client.ID, len(h.clients))

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.Send)
				log.Printf("[WebSocket] Client disconnected: %s (Total: %d)", client.ID, len(h.clients))
			}
			h.mu.Unlock()

		case message := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.Send <- message:
				default:
					close(client.Send)
					delete(h.clients, client)
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

func (c *Client) readPump(h *Hub) {
	defer func() {
		h.unregister <- c
		c.Conn.Close()
	}()

	for {
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			break
		}
		log.Printf("[WebSocket] Received from %s: %s", c.ID, string(message))

		// Broadcast received message to all connected clients
		h.broadcast <- message
	}
}

func (c *Client) writePump() {
	defer c.Conn.Close()

	for message := range c.Send {
		err := c.Conn.WriteMessage(websocket.TextMessage, message)
		if err != nil {
			break
		}
	}
}

var startTime = time.Now()

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	hub := NewHub()
	go hub.Run()

	mux := http.NewServeMux()

	// WebSocket Endpoint: /ws
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket Upgrade error: %v", err)
			return
		}

		clientID := fmt.Sprintf("client-%d", time.Now().UnixNano()%100000)
		client := &Client{
			ID:   clientID,
			Conn: conn,
			Send: make(chan []byte, 256),
		}

		hub.register <- client

		// Send welcome message to newly connected client
		welcomeMsg, _ := json.Marshal(map[string]interface{}{
			"type":      "welcome",
			"clientId":  clientID,
			"message":   "Connected to Go WebSocket Server",
			"timestamp": time.Now(),
		})
		client.Send <- welcomeMsg

		go client.writePump()
		go client.readPump(hub)
	})

	// Health Check API: /api/health
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":           "OK",
			"connectedClients": hub.ClientCount(),
			"uptime":           time.Since(startTime).Round(time.Second).String(),
			"timestamp":        time.Now(),
		})
	})

	// Web Test Client: /
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>Go WebSocket Server</title>
    <style>
        body { font-family: sans-serif; background: #12121a; color: #fff; padding: 30px; max-width: 700px; margin: auto; }
        .card { background: #1e1e2d; border-radius: 12px; padding: 24px; border: 1px solid #333; }
        .status { padding: 8px 14px; border-radius: 20px; font-weight: bold; display: inline-block; margin-bottom: 16px; }
        .online { background: #00ffcc22; color: #00ffcc; border: 1px solid #00ffcc; }
        .offline { background: #ff005522; color: #ff0055; border: 1px solid #ff0055; }
        #log { background: #0b0c10; border-radius: 8px; padding: 15px; height: 220px; overflow-y: auto; font-family: monospace; font-size: 13px; color: #a0f0ff; margin-bottom: 15px; border: 1px solid #222; }
        input { width: 70%; padding: 10px; border-radius: 6px; border: 1px solid #444; background: #2a2a3d; color: #fff; }
        button { padding: 10px 18px; border-radius: 6px; border: none; background: #00ffcc; color: #000; font-weight: bold; cursor: pointer; }
    </style>
</head>
<body>
    <div class="card">
        <h2>⚡ Go WebSocket Server</h2>
        <p style="color:#888; margin-bottom:15px;">WebSocket Endpoint: <code>ws://localhost:8080/ws</code></p>
        <div id="status" class="status offline">Disconnected</div>
        <div id="log"></div>
        <div>
            <input type="text" id="msgInput" placeholder="Type message to send via WebSocket...">
            <button onclick="send()">Send</button>
        </div>
    </div>
    <script>
        const log = document.getElementById('log');
        const status = document.getElementById('status');
        const input = document.getElementById('msgInput');
        const ws = new WebSocket((location.protocol === 'https:' ? 'wss://' : 'ws://') + location.host + '/ws');

        ws.onopen = () => {
            status.textContent = '● WebSocket Connected';
            status.className = 'status online';
        };
        ws.onmessage = (e) => {
            const div = document.createElement('div');
            div.textContent = '◀ ' + e.data;
            log.appendChild(div);
            log.scrollTop = log.scrollHeight;
        };
        ws.onclose = () => {
            status.textContent = '○ Disconnected';
            status.className = 'status offline';
        };

        function send() {
            if (input.value.trim() && ws.readyState === WebSocket.OPEN) {
                ws.send(input.value);
                const div = document.createElement('div');
                div.style.color = '#ffe600';
                div.textContent = '▶ Sent: ' + input.value;
                log.appendChild(div);
                input.value = '';
                log.scrollTop = log.scrollHeight;
            }
        }
        input.addEventListener('keydown', (e) => { if (e.key === 'Enter') send(); });
    </script>
</body>
</html>`
		w.Write([]byte(html))
	})

	addr := ":" + port
	fmt.Printf("====================================================\n")
	fmt.Printf("⚡ Go WebSocket Server is running on http://localhost:%s\n", port)
	fmt.Printf("📡 WebSocket URL: ws://localhost:%s/ws\n", port)
	fmt.Printf("📡 Health API:    http://localhost:%s/api/health\n", port)
	fmt.Printf("====================================================\n")

	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}
