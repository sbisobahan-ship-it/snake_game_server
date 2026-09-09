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
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow CORS
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
	totalSent      uint64
	msgPerSec      uint64
	lastSecCount   uint64
}

func NewHub() *Hub {
	h := &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte, 10000), // High capacity buffer for stress testing
		register:   make(chan *Client, 256),
		unregister: make(chan *Client, 256),
	}

	// Ticker to compute messages per second
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		for range ticker.C {
			current := atomic.LoadUint64(&h.totalReceived)
			last := h.lastSecCount
			h.lastSecCount = current
			if current >= last {
				atomic.StoreUint64(&h.msgPerSec, current-last)
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

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.Send)
			}
			h.mu.Unlock()

		case message := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.Send <- message:
					atomic.AddUint64(&h.totalSent, 1)
				default:
					// Drop if client buffer is full to prevent server locking
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
		atomic.AddUint64(&h.totalReceived, 1)

		// Forward to broadcast channel non-blockingly
		select {
		case h.broadcast <- message:
		default:
		}
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
			return
		}

		clientID := fmt.Sprintf("client-%d", time.Now().UnixNano()%1000000)
		client := &Client{
			ID:   clientID,
			Conn: conn,
			Send: make(chan []byte, 1024),
		}

		hub.register <- client

		go client.writePump()
		go client.readPump(hub)
	})

	// Server Real-time Stats API: /api/stats
	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)

		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":           "OK",
			"activeClients":    hub.ClientCount(),
			"totalReceived":    atomic.LoadUint64(&hub.totalReceived),
			"totalSent":        atomic.LoadUint64(&hub.totalSent),
			"messagesPerSec":   atomic.LoadUint64(&hub.msgPerSec),
			"uptime":           time.Since(startTime).Round(time.Second).String(),
			"goroutines":       runtime.NumGoroutine(),
			"memoryAllocMB":    fmt.Sprintf("%.2f MB", float64(mem.Alloc)/1024/1024),
			"timestamp":        time.Now(),
		})
	})

	// Web Dashboard with Stress Test Runner UI: /
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(htmlStressDashboard))
	})

	addr := ":" + port
	fmt.Printf("====================================================\n")
	fmt.Printf("⚡ High-Performance Go WebSocket Server Online!\n")
	fmt.Printf("🚀 Web Stress Test Dashboard : http://localhost:%s\n", port)
	fmt.Printf("📡 WebSocket Stream Endpoint  : ws://localhost:%s/ws\n", port)
	fmt.Printf("📊 Live Telemetry API         : http://localhost:%s/api/stats\n", port)
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

const htmlStressDashboard = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>🔥 High Pressure WebSocket Stress Runner</title>
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
    <link href="https://fonts.googleapis.com/css2?family=Outfit:wght@400;600;700;900&family=JetBrains+Mono:wght@500;700&display=swap" rel="stylesheet">
    <style>
        :root {
            --bg-dark: #0a0b12;
            --card-bg: rgba(18, 20, 32, 0.85);
            --neon-cyan: #00ffcc;
            --neon-pink: #ff0055;
            --neon-yellow: #ffe600;
            --neon-blue: #00b4d8;
            --border-color: rgba(0, 255, 204, 0.2);
            --text-main: #f0f4f8;
            --text-muted: #8e9bb0;
        }

        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: 'Outfit', sans-serif;
            background: var(--bg-dark);
            background-image: 
                radial-gradient(circle at 20% 10%, rgba(0, 255, 204, 0.08) 0%, transparent 40%),
                radial-gradient(circle at 80% 90%, rgba(255, 0, 85, 0.08) 0%, transparent 40%);
            color: var(--text-main);
            min-height: 100vh;
            padding: 24px;
        }

        .container {
            max-width: 1100px;
            margin: 0 auto;
        }

        header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            background: var(--card-bg);
            backdrop-filter: blur(12px);
            border: 1px solid var(--border-color);
            padding: 16px 24px;
            border-radius: 16px;
            margin-bottom: 24px;
            box-shadow: 0 10px 30px rgba(0, 0, 0, 0.5);
        }

        .header-title h1 {
            font-size: 22px;
            font-weight: 900;
            background: linear-gradient(135deg, var(--neon-cyan), var(--neon-blue));
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
        }

        .header-title span {
            font-size: 13px;
            color: var(--text-muted);
        }

        .status-badge {
            display: flex;
            align-items: center;
            gap: 8px;
            background: rgba(0, 0, 0, 0.4);
            padding: 8px 16px;
            border-radius: 30px;
            font-size: 13px;
            font-weight: 700;
            border: 1px solid rgba(255, 255, 255, 0.1);
        }

        .dot {
            width: 10px;
            height: 10px;
            border-radius: 50%;
            background: var(--neon-cyan);
            box-shadow: 0 0 10px var(--neon-cyan);
        }

        .dot.stressing {
            background: var(--neon-pink);
            box-shadow: 0 0 14px var(--neon-pink);
            animation: blink 0.5s infinite alternate;
        }

        @keyframes blink {
            from { opacity: 0.4; transform: scale(0.9); }
            to { opacity: 1; transform: scale(1.3); }
        }

        /* Metrics Grid */
        .stats-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
            gap: 16px;
            margin-bottom: 24px;
        }

        .stat-card {
            background: var(--card-bg);
            border: 1px solid var(--border-color);
            border-radius: 14px;
            padding: 20px;
            display: flex;
            flex-direction: column;
            gap: 6px;
            position: relative;
            overflow: hidden;
        }

        .stat-card::before {
            content: '';
            position: absolute;
            top: 0; left: 0; right: 0; height: 3px;
            background: linear-gradient(90deg, var(--neon-cyan), transparent);
        }

        .stat-card.danger::before {
            background: linear-gradient(90deg, var(--neon-pink), transparent);
        }

        .stat-card.warning::before {
            background: linear-gradient(90deg, var(--neon-yellow), transparent);
        }

        .stat-label {
            font-size: 12px;
            font-weight: 700;
            text-transform: uppercase;
            letter-spacing: 0.5px;
            color: var(--text-muted);
        }

        .stat-value {
            font-family: 'JetBrains Mono', monospace;
            font-size: 26px;
            font-weight: 900;
            color: var(--neon-cyan);
        }

        .stat-card.danger .stat-value { color: var(--neon-pink); }
        .stat-card.warning .stat-value { color: var(--neon-yellow); }

        /* Main Control Panel */
        .main-panel {
            display: grid;
            grid-template-columns: 1fr 1fr;
            gap: 24px;
            margin-bottom: 24px;
        }

        @media (max-width: 850px) {
            .main-panel { grid-template-columns: 1fr; }
        }

        .control-card {
            background: var(--card-bg);
            border: 1px solid var(--border-color);
            border-radius: 16px;
            padding: 24px;
        }

        .control-card h2 {
            font-size: 18px;
            margin-bottom: 16px;
            color: var(--neon-cyan);
            border-bottom: 1px solid rgba(255, 255, 255, 0.08);
            padding-bottom: 10px;
            display: flex;
            align-items: center;
            gap: 8px;
        }

        .form-group {
            margin-bottom: 18px;
        }

        .form-group label {
            display: flex;
            justify-content: space-between;
            font-size: 13px;
            font-weight: 700;
            margin-bottom: 8px;
            color: var(--text-main);
        }

        .form-group label span {
            color: var(--neon-yellow);
            font-family: 'JetBrains Mono', monospace;
        }

        input[type="range"] {
            width: 100%;
            accent-color: var(--neon-cyan);
            background: rgba(0,0,0,0.4);
            cursor: pointer;
        }

        select, input[type="text"] {
            width: 100%;
            background: rgba(0, 0, 0, 0.4);
            border: 1px solid rgba(255, 255, 255, 0.15);
            padding: 10px 14px;
            border-radius: 8px;
            color: #fff;
            font-family: 'Outfit', sans-serif;
            outline: none;
        }

        select:focus, input[type="text"]:focus {
            border-color: var(--neon-cyan);
        }

        .btn-toggle {
            width: 100%;
            padding: 16px;
            border-radius: 12px;
            font-size: 16px;
            font-weight: 900;
            letter-spacing: 0.5px;
            border: none;
            cursor: pointer;
            transition: all 0.2s ease;
            box-shadow: 0 4px 20px rgba(0, 0, 0, 0.4);
            display: flex;
            align-items: center;
            justify-content: center;
            gap: 10px;
        }

        .btn-start {
            background: linear-gradient(135deg, #00ffcc, #00b4d8);
            color: #050608;
        }

        .btn-start:hover {
            transform: translateY(-2px);
            box-shadow: 0 8px 25px rgba(0, 255, 204, 0.4);
        }

        .btn-stop {
            background: linear-gradient(135deg, #ff0055, #ff5500);
            color: #ffffff;
            animation: pulse-danger 1s infinite alternate;
        }

        @keyframes pulse-danger {
            from { box-shadow: 0 0 10px rgba(255, 0, 85, 0.5); }
            to { box-shadow: 0 0 25px rgba(255, 0, 85, 0.9); }
        }

        /* Live Terminal / Log Box */
        .log-box {
            background: #06070a;
            border: 1px solid rgba(255, 255, 255, 0.08);
            border-radius: 10px;
            height: 280px;
            overflow-y: auto;
            padding: 14px;
            font-family: 'JetBrains Mono', monospace;
            font-size: 12px;
            line-height: 1.6;
            color: #00ffcc;
        }

        .log-entry {
            display: flex;
            gap: 8px;
            margin-bottom: 4px;
            word-break: break-all;
        }

        .log-time { color: var(--text-muted); }
        .log-sent { color: var(--neon-yellow); }
        .log-recv { color: var(--neon-cyan); }
        .log-err { color: var(--neon-pink); }
    </style>
</head>
<body>
    <div class="container">
        <!-- Header -->
        <header>
            <div class="header-title">
                <h1>🔥 HIGH-PRESSURE WEBSOCKET RUNNER</h1>
                <span>Real-time Load Testing & Stress Generator</span>
            </div>
            <div class="status-badge">
                <span class="dot" id="statusDot"></span>
                <span id="statusLabel">IDLE / READY</span>
            </div>
        </header>

        <!-- Stats Bar -->
        <div class="stats-grid">
            <div class="stat-card">
                <span class="stat-label">Total Messages Sent</span>
                <span class="stat-value" id="valTotalSent">0</span>
            </div>
            <div class="stat-card warning">
                <span class="stat-label">Messages / Second (Speed)</span>
                <span class="stat-value" id="valMsgRate">0 msg/s</span>
            </div>
            <div class="stat-card danger">
                <span class="stat-label">Server Total Processed</span>
                <span class="stat-value" id="valServerProcessed">0</span>
            </div>
            <div class="stat-card">
                <span class="stat-label">Avg Latency / Ping</span>
                <span class="stat-value" id="valLatency">0 ms</span>
            </div>
        </div>

        <!-- Controls & Terminal -->
        <div class="main-panel">
            <!-- Left: Runner Controls -->
            <div class="control-card">
                <h2>⚙️ STRESS RUNNER CONFIG</h2>

                <div class="form-group">
                    <label>
                        Parallel Workers (Virtual Sockets)
                        <span id="txtWorkers">5 Workers</span>
                    </label>
                    <input type="range" id="rngWorkers" min="1" max="50" value="5">
                </div>

                <div class="form-group">
                    <label>
                        Sending Interval (Delay per worker)
                        <span id="txtDelay">10 ms (Turbo ~500 msg/s)</span>
                    </label>
                    <input type="range" id="rngDelay" min="1" max="200" value="10">
                </div>

                <div class="form-group">
                    <label>Packet Payload Type</label>
                    <select id="selPayload">
                        <option value="telemetry">High-Speed Telemetry JSON { x, y, speed, tick }</option>
                        <option value="binary_blob">Heavy String Flood (512 Bytes)</option>
                        <option value="minimal">Micro-packet (Ping/Pong)</option>
                    </select>
                </div>

                <button id="btnToggle" class="btn-toggle btn-start">
                    🚀 START STRESS RUNNER (অন করো)
                </button>
            </div>

            <!-- Right: Live Log Feed -->
            <div class="control-card">
                <h2>📊 LIVE ACTIVITY FEED</h2>
                <div class="log-box" id="logBox">
                    <div class="log-entry">
                        <span class="log-time">[System]</span>
                        <span>Stress testing tool initialized. Click Start to begin high-speed load generation.</span>
                    </div>
                </div>
            </div>
        </div>
    </div>

    <script>
        const btnToggle = document.getElementById('btnToggle');
        const statusDot = document.getElementById('statusDot');
        const statusLabel = document.getElementById('statusLabel');
        const rngWorkers = document.getElementById('rngWorkers');
        const txtWorkers = document.getElementById('txtWorkers');
        const rngDelay = document.getElementById('rngDelay');
        const txtDelay = document.getElementById('txtDelay');
        const selPayload = document.getElementById('selPayload');
        const logBox = document.getElementById('logBox');

        const valTotalSent = document.getElementById('valTotalSent');
        const valMsgRate = document.getElementById('valMsgRate');
        const valServerProcessed = document.getElementById('valServerProcessed');
        const valLatency = document.getElementById('valLatency');

        let isRunning = false;
        let workers = [];
        let totalSent = 0;
        let lastSentCount = 0;
        let currentRate = 0;
        let statInterval = null;

        // UI Sliders
        rngWorkers.addEventListener('input', () => {
            txtWorkers.textContent = rngWorkers.value + ' Workers';
        });

        rngDelay.addEventListener('input', () => {
            const delay = parseInt(rngDelay.value);
            const approxRate = Math.round((parseInt(rngWorkers.value) * 1000) / delay);
            txtDelay.textContent = delay + ' ms (~' + approxRate + ' msg/s)';
        });

        // Toggle Runner
        btnToggle.addEventListener('click', () => {
            if (!isRunning) {
                startRunner();
            } else {
                stopRunner();
            }
        });

        function addLog(type, text) {
            const row = document.createElement('div');
            row.className = 'log-entry';
            const time = new Date().toLocaleTimeString();
            row.innerHTML = '<span class="log-time">[' + time + ']</span> <span class="log-' + type + '">' + text + '</span>';
            logBox.appendChild(row);
            if (logBox.children.length > 100) {
                logBox.removeChild(logBox.firstChild);
            }
            logBox.scrollTop = logBox.scrollHeight;
        }

        function getPayload(workerId, seq) {
            const pType = selPayload.value;
            const now = Date.now();
            if (pType === 'telemetry') {
                return JSON.stringify({
                    worker: workerId,
                    seq: seq,
                    timestamp: now,
                    x: Math.floor(Math.random() * 800),
                    y: Math.floor(Math.random() * 600),
                    data: "PRESSURE_BURST_" + seq
                });
            } else if (pType === 'binary_blob') {
                return JSON.stringify({
                    worker: workerId,
                    seq: seq,
                    timestamp: now,
                    payload: "X".repeat(512)
                });
            } else {
                return "PING_" + now + "_" + seq;
            }
        }

        function startRunner() {
            isRunning = true;
            btnToggle.textContent = '🛑 STOP STRESS RUNNER (বন্ধ করো)';
            btnToggle.className = 'btn-toggle btn-stop';
            statusDot.className = 'dot stressing';
            statusLabel.textContent = 'HIGH PRESSURE RUNNING 🔥';

            const numWorkers = parseInt(rngWorkers.value);
            const delay = parseInt(rngDelay.value);
            const wsProtocol = location.protocol === 'https:' ? 'wss://' : 'ws://';
            const wsUrl = wsProtocol + location.host + '/ws';

            addLog('sent', '🔥 Starting ' + numWorkers + ' parallel WebSocket workers at ' + delay + 'ms interval...');

            workers = [];
            for (let i = 0; i < numWorkers; i++) {
                const workerId = 'W#' + (i + 1);
                const ws = new WebSocket(wsUrl);
                let seq = 0;
                let timer = null;

                ws.onopen = () => {
                    timer = setInterval(() => {
                        if (!isRunning || ws.readyState !== WebSocket.OPEN) return;
                        seq++;
                        const payload = getPayload(workerId, seq);
                        ws.send(payload);
                        totalSent++;
                    }, delay);
                };

                ws.onmessage = (e) => {
                    // Try to calculate round trip latency if JSON
                    try {
                        const data = JSON.parse(e.data);
                        if (data.timestamp) {
                            const lat = Date.now() - data.timestamp;
                            if (lat >= 0 && lat < 5000) {
                                valLatency.textContent = lat + ' ms';
                            }
                        }
                    } catch(err) {}
                };

                ws.onerror = () => {
                    addLog('err', workerId + ' socket error');
                };

                workers.push({ ws, timer });
            }

            // Stats calculator
            statInterval = setInterval(async () => {
                const diff = totalSent - lastSentCount;
                lastSentCount = totalSent;
                currentRate = diff;

                valTotalSent.textContent = totalSent.toLocaleString();
                valMsgRate.textContent = currentRate.toLocaleString() + ' msg/s';

                // Fetch server telemetry
                try {
                    const res = await fetch('/api/stats');
                    const stats = await res.json();
                    valServerProcessed.textContent = (stats.totalReceived || 0).toLocaleString();
                } catch(e) {}

                if (totalSent % 500 < 50) {
                    addLog('recv', '⚡ Sent batch: ' + totalSent + ' total messages @ ' + currentRate + ' msg/s');
                }
            }, 1000);
        }

        function stopRunner() {
            isRunning = false;
            btnToggle.textContent = '🚀 START STRESS RUNNER (অন করো)';
            btnToggle.className = 'btn-toggle btn-start';
            statusDot.className = 'dot';
            statusLabel.textContent = 'STOPPED / IDLE';

            workers.forEach(w => {
                if (w.timer) clearInterval(w.timer);
                if (w.ws) w.ws.close();
            });
            workers = [];

            if (statInterval) clearInterval(statInterval);
            addLog('err', '🛑 Stress Runner Stopped. Total Sent: ' + totalSent.toLocaleString() + ' messages.');
        }
    </script>
</body>
</html>
`
