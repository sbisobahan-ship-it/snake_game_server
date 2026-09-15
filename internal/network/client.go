package network

import (
	"encoding/json"
	"log"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 120 * time.Second // Extended to 120s to allow healthy margin for mobile networks
	pingPeriod     = 20 * time.Second  // Server keepalive ping interval
	maxMessageSize = 1024 * 64         // 64 KB max packet size
)

// WSMessage represents outgoing frame with its message type (Text or Binary)
type WSMessage struct {
	MsgType int
	Data    []byte
}

// Client represents a single active WebSocket connection
type Client struct {
	ID        string
	Conn      *websocket.Conn
	Send      chan WSMessage
	done      chan struct{} // Closed when client disconnects to instantly terminate WritePump
	onMessage func(c *Client, msgType int, raw []byte)
	onClose   func(c *Client)
	mu        sync.Mutex
	writeMu   sync.Mutex // Serializes all writes (WriteMessage and WriteControl) to avoid concurrent write collisions
	isClosed  bool
}

// NewClient creates a new client connection wrapper
func NewClient(id string, conn *websocket.Conn, onMessage func(c *Client, msgType int, raw []byte), onClose func(c *Client)) *Client {
	return &Client{
		ID:        id,
		Conn:      conn,
		Send:      make(chan WSMessage, 1024),
		done:      make(chan struct{}),
		onMessage: onMessage,
		onClose:   onClose,
	}
}

// safeWriteMessage writes a message with serialized writeMu protection
func (c *Client) safeWriteMessage(msgType int, data []byte) error {
	c.mu.Lock()
	if c.isClosed {
		c.mu.Unlock()
		return net.ErrClosed
	}
	c.mu.Unlock()

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
	return c.Conn.WriteMessage(msgType, data)
}

// safeWriteControl writes a control frame (e.g. Pong) with serialized writeMu protection
func (c *Client) safeWriteControl(msgType int, data []byte) error {
	c.mu.Lock()
	if c.isClosed {
		c.mu.Unlock()
		return net.ErrClosed
	}
	c.mu.Unlock()

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.Conn.WriteControl(msgType, data, time.Now().Add(writeWait))
}

// ReadPump listens for incoming WebSocket messages (Text or Binary)
func (c *Client) ReadPump() {
	defer func() {
		c.Close()
	}()

	c.Conn.SetReadLimit(maxMessageSize)
	c.Conn.SetReadDeadline(time.Now().Add(pongWait))

	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	// When client sends Ping (e.g. OkHttp pingInterval), reply with Pong safely without colliding with WritePump
	c.Conn.SetPingHandler(func(appData string) error {
		c.Conn.SetReadDeadline(time.Now().Add(pongWait))
		return c.safeWriteControl(websocket.PongMessage, []byte(appData))
	})

	for {
		msgType, message, err := c.Conn.ReadMessage()
		if err != nil {
			log.Printf("🔌 [Client Dropped] ID: %s (Reason: %v)", c.ID, err)
			break
		}
		c.Conn.SetReadDeadline(time.Now().Add(pongWait))

		if c.onMessage != nil {
			c.onMessage(c, msgType, message)
		}
	}
}

// WritePump writes outgoing messages (Text or Binary) to the WebSocket connection
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.Close()
	}()

	for {
		select {
		case <-c.done:
			// Instantly terminate WritePump when client connection is closed
			return

		case msg, ok := <-c.Send:
			if !ok {
				c.safeWriteControl(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.safeWriteMessage(msg.MsgType, msg.Data); err != nil {
				return
			}

		case <-ticker.C:
			if err := c.safeWriteControl(websocket.PingMessage, []byte{}); err != nil {
				return
			}
		}
	}
}

// SendJSON marshals and queues a JSON text message to client
func (c *Client) SendJSON(v interface{}) error {
	c.mu.Lock()
	if c.isClosed {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	data, err := json.Marshal(v)
	if err != nil {
		return err
	}

	c.queueMessage(WSMessage{MsgType: websocket.TextMessage, Data: data})
	return nil
}

// SendBinary queues a raw binary block message to client
func (c *Client) SendBinary(data []byte) {
	c.mu.Lock()
	if c.isClosed {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()

	c.queueMessage(WSMessage{MsgType: websocket.BinaryMessage, Data: data})
}

func (c *Client) queueMessage(msg WSMessage) {
	select {
	case c.Send <- msg:
	default:
		// Drop oldest frame if congested
		select {
		case <-c.Send:
		default:
		}
		select {
		case c.Send <- msg:
		default:
		}
	}
}

// Close gracefully closes the client connection
func (c *Client) Close() {
	c.mu.Lock()
	if c.isClosed {
		c.mu.Unlock()
		return
	}
	c.isClosed = true
	close(c.done) // Instantly terminate WritePump
	c.mu.Unlock()

	c.Conn.Close()
	if c.onClose != nil {
		c.onClose(c)
	}
}
