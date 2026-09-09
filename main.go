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
	ReadBufferSize:  65536, // 64 KB buffers for heavy data frames
	WriteBufferSize: 65536,
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
	totalBytesRecv uint64
	totalSent      uint64
	totalBytesSent uint64
	msgPerSec      uint64
	bytesPerSec    uint64
	lastSecCount   uint64
	lastSecBytes   uint64
}

func NewHub() *Hub {
	h := &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte, 50000), // Large buffer for heavy packet queues
		register:   make(chan *Client, 256),
		unregister: make(chan *Client, 256),
	}

	// High-speed telemetry calculation ticker
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		for range ticker.C {
			currMsgs := atomic.LoadUint64(&h.totalReceived)
			currBytes := atomic.LoadUint64(&h.totalBytesRecv)

			lastMsgs := h.lastSecCount
			lastBytes := h.lastSecBytes

			h.lastSecCount = currMsgs
			h.lastSecBytes = currBytes

			if currMsgs >= lastMsgs {
				atomic.StoreUint64(&h.msgPerSec, currMsgs-lastMsgs)
			}
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
			total := len(h.clients)
			h.mu.Unlock()

			sysMsg, _ := json.Marshal(map[string]interface{}{
				"type":      "system",
				"event":     "joined",
				"clientId":  client.ID,
				"message":   fmt.Sprintf("Client %s connected (Ready for Heavy Traffic)", client.ID),
				"online":    total,
				"timestamp": time.Now().Format("15:04:05.000"),
			})
			h.broadcastMessage(sysMsg)

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.Send)
				total := len(h.clients)
				h.mu.Unlock()

				sysMsg, _ := json.Marshal(map[string]interface{}{
					"type":      "system",
					"event":     "left",
					"clientId":  client.ID,
					"message":   fmt.Sprintf("Client %s disconnected", client.ID),
					"online":    total,
					"timestamp": time.Now().Format("15:04:05.000"),
				})
				h.broadcastMessage(sysMsg)
			} else {
				h.mu.Unlock()
			}

		case message := <-h.broadcast:
			msgLen := uint64(len(message))
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.Send <- message:
					atomic.AddUint64(&h.totalSent, 1)
					atomic.AddUint64(&h.totalBytesSent, msgLen)
				default:
					// Drop if consumer queue is overflowing to prevent blocking the entire engine
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *Hub) broadcastMessage(msg []byte) {
	select {
	case h.broadcast <- msg:
	default:
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

	// Support up to 10 MB payload per packet
	c.Conn.SetReadLimit(10 * 1024 * 1024)

	for {
		_, raw, err := c.Conn.ReadMessage()
		if err != nil {
			break
		}
		rawLen := uint64(len(raw))
		atomic.AddUint64(&h.totalReceived, 1)
		atomic.AddUint64(&h.totalBytesRecv, rawLen)

		// Broadcast received message to peers
		formatted, _ := json.Marshal(map[string]interface{}{
			"type":      "message",
			"sender":    c.ID,
			"payload":   string(raw),
			"size":      rawLen,
			"timestamp": time.Now().Format("15:04:05.000"),
		})

		h.broadcastMessage(formatted)
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

		clientID := fmt.Sprintf("peer-%d", time.Now().UnixNano()%10000)
		client := &Client{
			ID:   clientID,
			Conn: conn,
			Send: make(chan []byte, 4096),
		}

		hub.register <- client

		welcomeMsg, _ := json.Marshal(map[string]interface{}{
			"type":      "welcome",
			"clientId":  clientID,
			"message":   "Connected to Extreme WebSocket Stress Server",
			"timestamp": time.Now().Format("15:04:05.000"),
		})
		client.Send <- welcomeMsg

		go client.writePump()
		go client.readPump(hub)
	})

	// Live Server Stats API: /api/stats
	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)

		bytesRecv := atomic.LoadUint64(&hub.totalBytesRecv)
		bytesPerSec := atomic.LoadUint64(&hub.bytesPerSec)

		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":            "OK",
			"activeClients":     hub.ClientCount(),
			"totalReceived":     atomic.LoadUint64(&hub.totalReceived),
			"totalBytesRecv":    bytesRecv,
			"totalMBRecv":       fmt.Sprintf("%.2f MB", float64(bytesRecv)/(1024*1024)),
			"throughputMBs":     fmt.Sprintf("%.2f MB/s", float64(bytesPerSec)/(1024*1024)),
			"messagesPerSec":    atomic.LoadUint64(&hub.msgPerSec),
			"uptime":            time.Since(startTime).Round(time.Second).String(),
			"goroutines":        runtime.NumGoroutine(),
			"memoryAllocMB":     fmt.Sprintf("%.2f MB", float64(mem.Alloc)/(1024*1024)),
			"systemSysMemMB":    fmt.Sprintf("%.2f MB", float64(mem.Sys)/(1024*1024)),
			"gcCycles":          mem.NumGC,
			"timestamp":         time.Now(),
		})
	})

	// Web UI Dashboard: /
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(htmlHeavyStressDashboard))
	})

	addr := ":" + port
	fmt.Printf("==============================================================\n")
	fmt.Printf("🔥 Extreme Load & Heavy Payload WebSocket Server Online!\n")
	fmt.Printf("🌐 Stress Testing Dashboard : http://localhost:%s\n", port)
	fmt.Printf("📡 WebSocket Stream Endpoint : ws://localhost:%s/ws\n", port)
	fmt.Printf("📊 Live Telemetry API        : http://localhost:%s/api/stats\n", port)
	fmt.Printf("==============================================================\n")

	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}

const htmlHeavyStressDashboard = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>🔥 Extreme Heavy-Payload WebSocket Server Stress Tester</title>
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
    <link href="https://fonts.googleapis.com/css2?family=Outfit:wght@400;600;700;900&family=JetBrains+Mono:wght@400;700;900&display=swap" rel="stylesheet">
    <style>
        :root {
            --bg-dark: #06070c;
            --card-bg: rgba(14, 17, 28, 0.88);
            --neon-cyan: #00ffcc;
            --neon-pink: #ff0055;
            --neon-yellow: #ffe600;
            --neon-blue: #00b4d8;
            --neon-orange: #ff6b00;
            --border-color: rgba(255, 0, 85, 0.25);
            --text-main: #f1f5f9;
            --text-muted: #8e9bb0;
        }

        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: 'Outfit', sans-serif;
            background: var(--bg-dark);
            background-image: 
                radial-gradient(circle at 10% 10%, rgba(255, 0, 85, 0.12) 0%, transparent 40%),
                radial-gradient(circle at 90% 90%, rgba(0, 255, 204, 0.1) 0%, transparent 40%);
            color: var(--text-main);
            min-height: 100vh;
            padding: 20px;
        }

        .container {
            max-width: 1240px;
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
            margin-bottom: 20px;
            box-shadow: 0 10px 30px rgba(0, 0, 0, 0.6), 0 0 20px rgba(255, 0, 85, 0.1);
        }

        .header-title h1 {
            font-size: 22px;
            font-weight: 900;
            background: linear-gradient(135deg, var(--neon-pink), var(--neon-orange), var(--neon-yellow));
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
        }

        .header-title span {
            font-size: 13px;
            color: var(--text-muted);
        }

        .status-pill {
            display: flex;
            align-items: center;
            gap: 10px;
            background: rgba(0, 0, 0, 0.5);
            padding: 8px 18px;
            border-radius: 30px;
            font-size: 13px;
            border: 1px solid rgba(255, 255, 255, 0.1);
            font-weight: 700;
        }

        .pulse-dot {
            width: 12px;
            height: 12px;
            border-radius: 50%;
            background: var(--neon-cyan);
            box-shadow: 0 0 10px var(--neon-cyan);
        }

        .pulse-dot.stressing {
            background: var(--neon-pink);
            box-shadow: 0 0 18px var(--neon-pink);
            animation: hyper-pulse 0.3s infinite alternate;
        }

        @keyframes hyper-pulse {
            from { transform: scale(0.9); opacity: 0.5; }
            to { transform: scale(1.4); opacity: 1; }
        }

        /* Metrics Bar */
        .metrics-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(210px, 1fr));
            gap: 14px;
            margin-bottom: 20px;
        }

        .metric-card {
            background: var(--card-bg);
            border: 1px solid var(--border-color);
            border-radius: 14px;
            padding: 18px 20px;
            display: flex;
            flex-direction: column;
            gap: 4px;
            position: relative;
            overflow: hidden;
        }

        .metric-card::before {
            content: '';
            position: absolute;
            top: 0; left: 0; right: 0; height: 3px;
            background: linear-gradient(90deg, var(--neon-pink), var(--neon-orange));
        }

        .metric-card.cyan::before { background: linear-gradient(90deg, var(--neon-cyan), transparent); }
        .metric-card.yellow::before { background: linear-gradient(90deg, var(--neon-yellow), transparent); }

        .metric-label {
            font-size: 12px;
            font-weight: 700;
            color: var(--text-muted);
            text-transform: uppercase;
            letter-spacing: 0.5px;
        }

        .metric-val {
            font-family: 'JetBrains Mono', monospace;
            font-size: 26px;
            font-weight: 900;
            color: var(--neon-pink);
        }

        .metric-card.cyan .metric-val { color: var(--neon-cyan); }
        .metric-card.yellow .metric-val { color: var(--neon-yellow); }

        /* Workspace */
        .workspace {
            display: grid;
            grid-template-columns: 420px 1fr;
            gap: 20px;
        }

        @media (max-width: 980px) {
            .workspace { grid-template-columns: 1fr; }
        }

        .panel {
            background: var(--card-bg);
            border: 1px solid var(--border-color);
            border-radius: 16px;
            padding: 22px;
            display: flex;
            flex-direction: column;
        }

        .panel-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 18px;
            border-bottom: 1px solid rgba(255, 255, 255, 0.08);
            padding-bottom: 12px;
        }

        .panel-header h2 {
            font-size: 18px;
            font-weight: 800;
            color: var(--neon-pink);
            display: flex;
            align-items: center;
            gap: 8px;
        }

        .form-row {
            margin-bottom: 16px;
        }

        .form-row label {
            display: flex;
            justify-content: space-between;
            font-size: 13px;
            font-weight: 700;
            margin-bottom: 6px;
        }

        .form-row label span {
            color: var(--neon-yellow);
            font-family: 'JetBrains Mono', monospace;
        }

        input[type="range"] {
            width: 100%;
            accent-color: var(--neon-pink);
            cursor: pointer;
        }

        select {
            width: 100%;
            background: rgba(0, 0, 0, 0.5);
            border: 1px solid rgba(255, 255, 255, 0.15);
            padding: 10px 14px;
            border-radius: 8px;
            color: #fff;
            font-family: 'Outfit', sans-serif;
            font-size: 13px;
            outline: none;
        }

        select:focus {
            border-color: var(--neon-pink);
        }

        .btn-flood {
            width: 100%;
            padding: 16px;
            border-radius: 12px;
            font-size: 16px;
            font-weight: 900;
            letter-spacing: 0.5px;
            border: none;
            cursor: pointer;
            transition: all 0.2s;
            margin-top: 10px;
            box-shadow: 0 4px 20px rgba(0, 0, 0, 0.5);
        }

        .btn-start {
            background: linear-gradient(135deg, #ff0055, #ff6b00);
            color: #fff;
        }

        .btn-start:hover {
            transform: translateY(-2px);
            box-shadow: 0 8px 30px rgba(255, 0, 85, 0.5);
        }

        .btn-stop {
            background: linear-gradient(135deg, #ff0055, #99002b);
            color: #ffffff;
            animation: pulse-danger 0.5s infinite alternate;
        }

        @keyframes pulse-danger {
            from { box-shadow: 0 0 10px rgba(255, 0, 85, 0.6); }
            to { box-shadow: 0 0 30px rgba(255, 0, 85, 1); }
        }

        /* Live Stream / Packet Inspector */
        .stream-panel {
            display: flex;
            flex-direction: column;
            height: 620px;
        }

        .feed-box {
            flex: 1;
            background: #040508;
            border: 1px solid rgba(255, 255, 255, 0.08);
            border-radius: 12px;
            padding: 14px;
            overflow-y: auto;
            font-family: 'JetBrains Mono', monospace;
            font-size: 12px;
            line-height: 1.6;
            display: flex;
            flex-direction: column;
            gap: 6px;
        }

        .packet-row {
            padding: 8px 12px;
            background: rgba(255, 255, 255, 0.02);
            border-radius: 6px;
            border-left: 4px solid var(--neon-pink);
            display: flex;
            flex-direction: column;
            gap: 4px;
            word-break: break-all;
        }

        .packet-header {
            display: flex;
            justify-content: space-between;
            font-size: 11px;
        }

        .packet-size {
            color: var(--neon-yellow);
            font-weight: 800;
        }

        .packet-preview {
            color: #cbd5e1;
            font-size: 12px;
            max-height: 40px;
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
        }

        .btn-clear {
            background: rgba(255, 255, 255, 0.08);
            color: var(--text-muted);
            padding: 6px 12px;
            font-size: 12px;
            border-radius: 6px;
            border: none;
            cursor: pointer;
        }

        .btn-clear:hover { background: rgba(255, 255, 255, 0.15); color: #fff; }
    </style>
</head>
<body>
    <div class="container">
        <!-- Header -->
        <header>
            <div class="header-title">
                <h1>🔥 EXTREME HEAVY-PAYLOAD WEBSOCKET STRESS TESTER</h1>
                <span>Massive Multi-Kilobyte Real-Time Millisecond Pressure Generator</span>
            </div>
            <div class="status-pill">
                <span class="pulse-dot" id="statusDot"></span>
                <span id="statusLabel">IDLE / READY</span>
                <span>|</span>
                <span>⚡ RAM: <strong id="valServerMem" style="color:var(--neon-cyan)">-- MB</strong></span>
            </div>
        </header>

        <!-- Metrics Grid -->
        <div class="metrics-grid">
            <div class="metric-card">
                <span class="metric-label">Bandwidth Throughput</span>
                <span class="metric-val" id="valThroughput">0.00 MB/s</span>
            </div>
            <div class="metric-card yellow">
                <span class="metric-label">Live Packet Rate</span>
                <span class="metric-val" id="valPacketRate">0 msg/s</span>
            </div>
            <div class="metric-card">
                <span class="metric-label">Total Data Ingested</span>
                <span class="metric-val" id="valTotalData">0.00 MB</span>
            </div>
            <div class="metric-card cyan">
                <span class="metric-label">Total Packets Processed</span>
                <span class="metric-val" id="valTotalPackets">0</span>
            </div>
        </div>

        <!-- Main Workspace -->
        <div class="workspace">
            <!-- Left: Heavy Generator Controls -->
            <div class="panel">
                <div class="panel-header">
                    <h2>⚙️ HEAVY PRESSURE ENGINE</h2>
                </div>

                <!-- Payload Size Selector -->
                <div class="form-row">
                    <label>
                        Payload Data Size per Packet
                        <span id="txtPayloadSize">25 KB per packet</span>
                    </label>
                    <select id="selPayload">
                        <option value="5">🔥 5 KB - Heavy Game Physics Frame (100 Entities)</option>
                        <option value="25" selected>💥 25 KB - Massive Multi-State Array (500 Nodes)</option>
                        <option value="50">⚡ 50 KB - Ultra Monster Telemetry (1,000 JSON Objects)</option>
                        <option value="100">🌋 100 KB - Giant Blob Avalanche (2,000 Complex Structs)</option>
                        <option value="250">💣 250 KB - Mega Memory Thrash Buffer</option>
                    </select>
                </div>

                <!-- Millisecond Delay -->
                <div class="form-row">
                    <label>
                        Sending Interval (Delay per burst)
                        <span id="txtDelay">5 ms (Hyper ~200 Burst/s)</span>
                    </label>
                    <input type="range" id="rngDelay" min="1" max="100" value="5">
                </div>

                <!-- Concurrent Sockets -->
                <div class="form-row">
                    <label>
                        Concurrent Parallel Sockets
                        <span id="txtWorkers">5 Parallel Sockets</span>
                    </label>
                    <input type="range" id="rngWorkers" min="1" max="25" value="5">
                </div>

                <!-- Estimated Pressure -->
                <div style="background:rgba(0,0,0,0.4); border:1px solid rgba(255,255,255,0.06); padding:12px; border-radius:8px; margin-bottom:16px; font-size:12px; font-family:'JetBrains Mono',monospace;">
                    <div style="color:var(--neon-yellow); margin-bottom:4px;">📊 ESTIMATED TARGET PRESSURE:</div>
                    <div style="color:var(--text-main)" id="txtTargetPressure">~25.0 MB/s Bandwidth Load</div>
                </div>

                <button id="btnToggle" class="btn-flood btn-start">
                    🚀 START EXTREME PRESSURE (হাই প্রেশারে বড় ডাটা পাঠাও)
                </button>
            </div>

            <!-- Right: Live Packet Stream -->
            <div class="panel stream-panel">
                <div class="panel-header">
                    <h2 style="color:var(--neon-cyan)">📺 REAL-TIME PACKET RENDER STREAM</h2>
                    <div style="display:flex; gap:10px; align-items:center;">
                        <button class="btn-clear" id="btnClear">Clear Feed</button>
                    </div>
                </div>

                <div class="feed-box" id="feedBox">
                    <div class="packet-row" style="border-left-color:var(--neon-cyan);">
                        <div class="packet-header">
                            <span style="color:var(--neon-cyan)">[SYSTEM] Connected to Extreme Go Server</span>
                            <span class="packet-size">READY</span>
                        </div>
                        <div class="packet-preview">Server read buffer configured to 64 KB, packet limit up to 10 MB.</div>
                    </div>
                </div>
            </div>
        </div>
    </div>

    <script>
        const btnToggle = document.getElementById('btnToggle');
        const statusDot = document.getElementById('statusDot');
        const statusLabel = document.getElementById('statusLabel');
        const valServerMem = document.getElementById('valServerMem');
        const valThroughput = document.getElementById('valThroughput');
        const valPacketRate = document.getElementById('valPacketRate');
        const valTotalData = document.getElementById('valTotalData');
        const valTotalPackets = document.getElementById('valTotalPackets');

        const selPayload = document.getElementById('selPayload');
        const txtPayloadSize = document.getElementById('txtPayloadSize');
        const rngDelay = document.getElementById('rngDelay');
        const txtDelay = document.getElementById('txtDelay');
        const rngWorkers = document.getElementById('rngWorkers');
        const txtWorkers = document.getElementById('txtWorkers');
        const txtTargetPressure = document.getElementById('txtTargetPressure');
        const feedBox = document.getElementById('feedBox');
        const btnClear = document.getElementById('btnClear');

        let isRunning = false;
        let sockets = [];
        let totalSentPackets = 0;
        let totalSentBytes = 0;
        let lastBytes = 0;
        let lastPackets = 0;
        let statTicker = null;

        function updateEstimates() {
            const kb = parseInt(selPayload.value);
            const delay = parseInt(rngDelay.value);
            const workers = parseInt(rngWorkers.value);
            const pps = Math.round((workers * 1000) / delay);
            const mbs = ((pps * kb) / 1024).toFixed(1);
            txtPayloadSize.textContent = kb + ' KB per packet';
            txtDelay.textContent = delay + ' ms (~' + pps + ' Packets/s)';
            txtWorkers.textContent = workers + ' Parallel Sockets';
            txtTargetPressure.textContent = '~' + mbs + ' MB/s Bandwidth (' + pps.toLocaleString() + ' Packets/s)';
        }

        selPayload.addEventListener('change', updateEstimates);
        rngDelay.addEventListener('input', updateEstimates);
        rngWorkers.addEventListener('input', updateEstimates);
        updateEstimates();

        // Heavy Payload Generator (creates realistic complex data matrices)
        function generateHeavyPayload(kbSize, workerId, seq) {
            const nodeCount = Math.floor((kbSize * 1024) / 120); // ~120 bytes per JSON element
            const nodes = [];
            const now = Date.now();

            for (let i = 0; i < nodeCount; i++) {
                nodes.push({
                    id: 'node_' + i,
                    x: (Math.random() * 2000).toFixed(2),
                    y: (Math.random() * 2000).toFixed(2),
                    vx: (Math.random() * 10 - 5).toFixed(2),
                    vy: (Math.random() * 10 - 5).toFixed(2),
                    hp: 100,
                    hash: '0x' + Math.random().toString(16).substr(2, 8)
                });
            }

            return JSON.stringify({
                stressTest: "EXTREME_PRESSURE_BURST",
                worker: workerId,
                seq: seq,
                timestamp: now,
                payloadSizeKB: kbSize,
                matrixCount: nodes.length,
                entities: nodes
            });
        }

        btnToggle.addEventListener('click', () => {
            if (!isRunning) startExtremeStress();
            else stopExtremeStress();
        });

        function startExtremeStress() {
            isRunning = true;
            btnToggle.textContent = '🛑 STOP PRESSURE (বন্ধ করো)';
            btnToggle.className = 'btn-flood btn-stop';
            statusDot.className = 'pulse-dot stressing';
            statusLabel.textContent = 'MAX PRESSURE FLOOD ACTIVE 🔥';

            const numWorkers = parseInt(rngWorkers.value);
            const delay = parseInt(rngDelay.value);
            const kbSize = parseInt(selPayload.value);
            const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
            const wsUrl = proto + '//' + location.host + '/ws';

            sockets = [];
            for (let i = 0; i < numWorkers; i++) {
                const workerId = 'W#' + (i + 1);
                const ws = new WebSocket(wsUrl);
                let seq = 0;
                let timer = null;

                ws.onopen = () => {
                    timer = setInterval(() => {
                        if (!isRunning || ws.readyState !== WebSocket.OPEN) return;
                        seq++;
                        const payload = generateHeavyPayload(kbSize, workerId, seq);
                        ws.send(payload);
                        totalSentPackets++;
                        totalSentBytes += payload.length;
                    }, delay);
                };

                ws.onmessage = (e) => {
                    renderPacket(e.data);
                };

                sockets.push({ ws, timer });
            }

            // High-frequency telemetry sampler
            statTicker = setInterval(async () => {
                const diffBytes = totalSentBytes - lastBytes;
                const diffPackets = totalSentPackets - lastPackets;
                lastBytes = totalSentBytes;
                lastPackets = totalSentPackets;

                const liveMBs = (diffBytes / (1024 * 1024)).toFixed(2);
                valThroughput.textContent = liveMBs + ' MB/s';
                valPacketRate.textContent = diffPackets.toLocaleString() + ' msg/s';
                valTotalData.textContent = (totalSentBytes / (1024 * 1024)).toFixed(2) + ' MB';
                valTotalPackets.textContent = totalSentPackets.toLocaleString();

                try {
                    const res = await fetch('/api/stats');
                    const stats = await res.json();
                    if (stats.memoryAllocMB) valServerMem.textContent = stats.memoryAllocMB;
                } catch(e) {}
            }, 1000);
        }

        function stopExtremeStress() {
            isRunning = false;
            btnToggle.textContent = '🚀 START EXTREME PRESSURE (হাই প্রেশারে বড় ডাটা পাঠাও)';
            btnToggle.className = 'btn-flood btn-start';
            statusDot.className = 'pulse-dot';
            statusLabel.textContent = 'STOPPED / IDLE';

            sockets.forEach(s => {
                if (s.timer) clearInterval(s.timer);
                if (s.ws) s.ws.close();
            });
            sockets = [];

            if (statTicker) clearInterval(statTicker);
        }

        let renderThrottle = 0;
        function renderPacket(raw) {
            renderThrottle++;
            // Sample packets for UI to avoid freezing browser DOM under 50 MB/s load
            if (renderThrottle % 3 !== 0) return;

            try {
                const data = JSON.parse(raw);
                if (data.type === 'message') {
                    const sizeKB = (data.size / 1024).toFixed(1);
                    const row = document.createElement('div');
                    row.className = 'packet-row';
                    row.innerHTML = 
                        '<div class="packet-header">' +
                            '<span style="color:var(--neon-pink)">🔥 [' + data.timestamp + '] SENDER: ' + data.sender + '</span>' +
                            '<span class="packet-size">📦 ' + sizeKB + ' KB (' + data.size.toLocaleString() + ' Bytes)</span>' +
                        '</div>' +
                        '<div class="packet-preview">' + escapeHtml(data.payload.substring(0, 180)) + '...</div>';

                    feedBox.appendChild(row);
                    if (feedBox.children.length > 50) {
                        feedBox.removeChild(feedBox.firstChild);
                    }
                    feedBox.scrollTop = feedBox.scrollHeight;
                }
            } catch(e) {}
        }

        function escapeHtml(str) {
            const div = document.createElement('div');
            div.innerText = str;
            return div.innerHTML;
        }

        btnClear.addEventListener('click', () => { feedBox.innerHTML = ''; });
    </script>
</body>
</html>
`
