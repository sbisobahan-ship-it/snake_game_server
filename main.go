package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024 * 1024, // 1 MB buffer
	WriteBufferSize: 1024 * 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins
	},
}

type Client struct {
	ID   string
	Conn *websocket.Conn
	Send chan []byte
}

type Hub struct {
	clients        map[*Client]bool
	broadcast      chan []byte
	register       chan *Client
	unregister     chan *Client
	mu             sync.RWMutex
	totalReceived  uint64
	totalBytesRecv uint64
	totalSent      uint64
	totalBytesSent uint64
	bytesPerSec    uint64
	lastSecBytes   uint64
}

func NewHub() *Hub {
	h := &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte, 1000), // Buffer for 1MB frames
		register:   make(chan *Client, 128),
		unregister: make(chan *Client, 128),
	}

	// Throughput calculation ticker
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		for range ticker.C {
			currBytes := atomic.LoadUint64(&h.totalBytesRecv)
			lastBytes := h.lastSecBytes
			h.lastSecBytes = currBytes

			if currBytes >= lastBytes {
				atomic.StoreUint64(&h.bytesPerSec, currBytes-lastBytes)
			}
		}
	}()

	return h
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			log.Printf("🔥 [Peer Connected] %s | Active Clients: %d", client.ID, h.ClientCount())

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.Send)
			}
			h.mu.Unlock()
			log.Printf("🛑 [Peer Disconnected] %s | Active Clients: %d", client.ID, h.ClientCount())

		case message := <-h.broadcast:
			msgLen := uint64(len(message))
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.Send <- message:
					atomic.AddUint64(&h.totalSent, 1)
					atomic.AddUint64(&h.totalBytesSent, msgLen)
				default:
					// Skip if client can't keep up with 1MB flood
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

	// Allow up to 10 MB per frame
	c.Conn.SetReadLimit(10 * 1024 * 1024)

	for {
		_, raw, err := c.Conn.ReadMessage()
		if err != nil {
			break
		}
		rawLen := uint64(len(raw))
		atomic.AddUint64(&h.totalReceived, 1)
		atomic.AddUint64(&h.totalBytesRecv, rawLen)

		// Broadcast to all other connected peers
		select {
		case h.broadcast <- raw:
		default:
		}
	}
}

func (c *Client) writePump() {
	defer c.Conn.Close()

	for message := range c.Send {
		err := c.Conn.WriteMessage(websocket.BinaryMessage, message)
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
			return
		}

		clientID := fmt.Sprintf("peer-%d", time.Now().UnixNano()%100000)
		client := &Client{
			ID:   clientID,
			Conn: conn,
			Send: make(chan []byte, 100),
		}

		hub.register <- client

		go client.writePump()
		go client.readPump(hub)
	})

	// Server Stats API: /api/stats
	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)

		bytesRecv := atomic.LoadUint64(&hub.totalBytesRecv)
		bytesPerSec := atomic.LoadUint64(&hub.bytesPerSec)

		json.NewEncoder(w).Encode(map[string]interface{}{
			"activeClients":  hub.ClientCount(),
			"totalMBRecv":    fmt.Sprintf("%.2f MB", float64(bytesRecv)/(1024*1024)),
			"throughputMBs":  fmt.Sprintf("%.2f MB/s", float64(bytesPerSec)/(1024*1024)),
			"totalPackets":   atomic.LoadUint64(&hub.totalReceived),
			"serverMemAlloc": fmt.Sprintf("%.2f MB", float64(mem.Alloc)/(1024*1024)),
			"uptime":         time.Since(startTime).Round(time.Second).String(),
		})
	})

	// Clean Dark Page with Automatic 1MB Flooder: /
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(htmlDarkAutoFlooder))
	})

	addr := ":" + port
	fmt.Printf("==============================================================\n")
	fmt.Printf("⚡ 1MB Auto-Flooding Dark WebSocket Server Online!\n")
	fmt.Printf("🌐 Dark Auto-Flood Page      : http://localhost:%s\n", port)
	fmt.Printf("📡 WebSocket Stream Endpoint : ws://localhost:%s/ws\n", port)
	fmt.Printf("📊 Live Telemetry API        : http://localhost:%s/api/stats\n", port)
	fmt.Printf("==============================================================\n")

	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}

const htmlDarkAutoFlooder = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>1MB High-Frequency WebSocket Auto-Flooder</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            background-color: #000000;
            color: #00ffcc;
            font-family: 'Courier New', Courier, monospace;
            min-height: 100vh;
            display: flex;
            flex-direction: column;
            justify-content: center;
            align-items: center;
            padding: 20px;
            overflow: hidden;
            user-select: none;
        }

        .flood-indicator {
            position: fixed;
            top: 20px;
            left: 20px;
            display: flex;
            align-items: center;
            gap: 10px;
            font-size: 14px;
            color: #ff0055;
            font-weight: bold;
            letter-spacing: 2px;
        }

        .dot {
            width: 14px;
            height: 14px;
            border-radius: 50%;
            background: #ff0055;
            box-shadow: 0 0 20px #ff0055, 0 0 40px #ff0055;
            animation: pulse 0.3s infinite alternate;
        }

        @keyframes pulse {
            from { transform: scale(0.8); opacity: 0.4; }
            to { transform: scale(1.4); opacity: 1; }
        }

        .telemetry-board {
            text-align: center;
            display: flex;
            flex-direction: column;
            gap: 25px;
            z-index: 10;
        }

        .metric-speed {
            font-size: clamp(3rem, 10vw, 7rem);
            font-weight: 900;
            color: #ffffff;
            text-shadow: 0 0 30px rgba(0, 255, 204, 0.8), 0 0 60px rgba(0, 255, 204, 0.4);
            letter-spacing: -2px;
            line-height: 1;
        }

        .speed-label {
            font-size: 16px;
            color: #8899aa;
            letter-spacing: 4px;
            text-transform: uppercase;
        }

        .stats-row {
            display: flex;
            gap: 40px;
            justify-content: center;
            flex-wrap: wrap;
            margin-top: 10px;
        }

        .stat-box {
            background: rgba(255, 255, 255, 0.03);
            border: 1px solid rgba(0, 255, 204, 0.15);
            padding: 16px 28px;
            border-radius: 8px;
            display: flex;
            flex-direction: column;
            gap: 6px;
            min-width: 180px;
        }

        .stat-box.sent { border-color: rgba(255, 0, 85, 0.3); }
        .stat-box.sent .val { color: #ff0055; text-shadow: 0 0 15px rgba(255,0,85,0.6); }

        .stat-box.recv { border-color: rgba(255, 230, 0, 0.3); }
        .stat-box.recv .val { color: #ffe600; text-shadow: 0 0 15px rgba(255,230,0,0.6); }

        .stat-box .lbl {
            font-size: 11px;
            color: #667788;
            letter-spacing: 2px;
        }

        .stat-box .val {
            font-size: 24px;
            font-weight: bold;
            color: #00ffcc;
        }

        .matrix-bg {
            position: absolute;
            inset: 0;
            background: radial-gradient(circle at center, rgba(255,0,85,0.06) 0%, rgba(0,0,0,0.98) 70%);
            pointer-events: none;
        }

        .footer-info {
            position: fixed;
            bottom: 20px;
            font-size: 12px;
            color: #445566;
            letter-spacing: 1px;
        }
    </style>
</head>
<body>
    <div class="matrix-bg"></div>

    <div class="flood-indicator">
        <div class="dot"></div>
        <span>AUTO 1MB FLOODING ACTIVE</span>
    </div>

    <div class="telemetry-board">
        <div class="speed-label">LIVE THROUGHPUT / SPEED</div>
        <div class="metric-speed" id="liveSpeed">0.00 MB/s</div>

        <div class="stats-row">
            <div class="stat-box sent">
                <span class="lbl">TOTAL 1MB SENT</span>
                <span class="val" id="valSent">0 MB</span>
            </div>
            <div class="stat-box recv">
                <span class="lbl">TOTAL 1MB RECEIVED</span>
                <span class="val" id="valRecv">0 MB</span>
            </div>
            <div class="stat-box">
                <span class="lbl">CONNECTED PEERS</span>
                <span class="val" id="valPeers">1</span>
            </div>
            <div class="stat-box">
                <span class="lbl">SERVER TOTAL MB</span>
                <span class="val" id="valServerTotal">0 MB</span>
            </div>
        </div>
    </div>

    <div class="footer-info">
        PAYLOAD: 1,048,576 BYTES (1 MB) | INTERVAL: MILLISECOND CONTINUOUS LOOP
    </div>

    <script>
        const liveSpeed = document.getElementById('liveSpeed');
        const valSent = document.getElementById('valSent');
        const valRecv = document.getElementById('valRecv');
        const valPeers = document.getElementById('valPeers');
        const valServerTotal = document.getElementById('valServerTotal');

        let totalSentBytes = 0;
        let totalRecvBytes = 0;
        let lastSecondSentBytes = 0;
        let lastSecondRecvBytes = 0;

        // Generate 1 Megabyte (1,048,576 Bytes) Binary Chunk
        const ONE_MB = 1024 * 1024;
        const oneMbBuffer = new Uint8Array(ONE_MB);
        for (let i = 0; i < ONE_MB; i++) {
            oneMbBuffer[i] = (i % 256);
        }

        const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = proto + '//' + location.host + '/ws';
        let ws = null;

        function connect() {
            ws = new WebSocket(wsUrl);
            ws.binaryType = 'arraybuffer';

            ws.onopen = () => {
                // Continuous millisecond sending loop as soon as connected
                floodLoop();
            };

            ws.onmessage = (event) => {
                const byteLength = event.data.byteLength || event.data.length || ONE_MB;
                totalRecvBytes += byteLength;
            };

            ws.onclose = () => {
                setTimeout(connect, 1000);
            };

            ws.onerror = (e) => {};
        }

        // High frequency send loop
        function floodLoop() {
            if (!ws || ws.readyState !== WebSocket.OPEN) return;

            // Send 1 MB chunk if buffer is not excessively backpressured
            if (ws.bufferedAmount < 4 * ONE_MB) {
                ws.send(oneMbBuffer);
                totalSentBytes += ONE_MB;
            }

            // Immediately schedule next send (millisecond loop)
            setTimeout(floodLoop, 1);
        }

        // Telemetry updater
        setInterval(async () => {
            const diffSent = totalSentBytes - lastSecondSentBytes;
            const diffRecv = totalRecvBytes - lastSecondRecvBytes;
            lastSecondSentBytes = totalSentBytes;
            lastSecondRecvBytes = totalRecvBytes;

            const totalSpeedMBs = ((diffSent + diffRecv) / (1024 * 1024)).toFixed(2);
            liveSpeed.textContent = totalSpeedMBs + ' MB/s';

            valSent.textContent = (totalSentBytes / (1024 * 1024)).toFixed(0) + ' MB';
            valRecv.textContent = (totalRecvBytes / (1024 * 1024)).toFixed(0) + ' MB';

            try {
                const res = await fetch('/api/stats');
                const data = await res.json();
                if (data.activeClients !== undefined) valPeers.textContent = data.activeClients;
                if (data.totalMBRecv) valServerTotal.textContent = data.totalMBRecv;
            } catch(e) {}
        }, 1000);

        // Start immediately
        connect();
    </script>
</body>
</html>
`
