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
	ReadBufferSize:  8192,
	WriteBufferSize: 8192,
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
	clients       map[*Client]bool
	broadcast     chan []byte
	register      chan *Client
	unregister    chan *Client
	mu            sync.RWMutex
	totalReceived uint64
	totalSent     uint64
	msgPerSec     uint64
	lastSecCount  uint64
}

func NewHub() *Hub {
	h := &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte, 20000), // Large capacity buffer for high-frequency broadcasting
		register:   make(chan *Client, 256),
		unregister: make(chan *Client, 256),
	}

	// Speed calculation ticker
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
			total := len(h.clients)
			h.mu.Unlock()

			// Broadcast client join event to all peers
			sysMsg, _ := json.Marshal(map[string]interface{}{
				"type":      "system",
				"event":     "joined",
				"clientId":  client.ID,
				"message":   fmt.Sprintf("Client %s connected", client.ID),
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

				// Broadcast client leave event
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
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.Send <- message:
					atomic.AddUint64(&h.totalSent, 1)
				default:
					// Don't block hub if one slow consumer lags
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

	for {
		_, raw, err := c.Conn.ReadMessage()
		if err != nil {
			break
		}
		atomic.AddUint64(&h.totalReceived, 1)

		// Format message with sender metadata & timestamp
		formatted, _ := json.Marshal(map[string]interface{}{
			"type":      "message",
			"sender":    c.ID,
			"payload":   string(raw),
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
			Send: make(chan []byte, 2048),
		}

		hub.register <- client

		// Welcome handshake
		welcomeMsg, _ := json.Marshal(map[string]interface{}{
			"type":      "welcome",
			"clientId":  clientID,
			"message":   "Connected to Real-time WebSocket Hub",
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

	// Web UI Dashboard with Real-time Live Rendering: /
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(htmlDashboard))
	})

	addr := ":" + port
	fmt.Printf("====================================================\n")
	fmt.Printf("⚡ Real-time WebSocket Live Render Server Online!\n")
	fmt.Printf("🌐 Live Stream Dashboard : http://localhost:%s\n", port)
	fmt.Printf("📡 WebSocket Endpoint     : ws://localhost:%s/ws\n", port)
	fmt.Printf("📊 Live Telemetry API     : http://localhost:%s/api/stats\n", port)
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

const htmlDashboard = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>📡 Live Real-Time WebSocket Message Stream</title>
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
    <link href="https://fonts.googleapis.com/css2?family=Outfit:wght@400;600;700;900&family=JetBrains+Mono:wght@400;600;800&display=swap" rel="stylesheet">
    <style>
        :root {
            --bg-dark: #07080d;
            --card-bg: rgba(14, 17, 27, 0.85);
            --neon-cyan: #00ffcc;
            --neon-pink: #ff0055;
            --neon-yellow: #ffe600;
            --neon-blue: #00b4d8;
            --neon-green: #10b981;
            --border-color: rgba(0, 255, 204, 0.2);
            --text-main: #f3f6fa;
            --text-muted: #8896ab;
        }

        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: 'Outfit', sans-serif;
            background: var(--bg-dark);
            background-image: 
                radial-gradient(circle at 15% 15%, rgba(0, 255, 204, 0.08) 0%, transparent 40%),
                radial-gradient(circle at 85% 85%, rgba(255, 0, 85, 0.08) 0%, transparent 40%);
            color: var(--text-main);
            min-height: 100vh;
            padding: 20px;
        }

        .container {
            max-width: 1200px;
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

        .peer-badge {
            display: flex;
            align-items: center;
            gap: 12px;
            background: rgba(0, 0, 0, 0.5);
            padding: 8px 18px;
            border-radius: 30px;
            font-size: 13px;
            border: 1px solid rgba(255, 255, 255, 0.1);
        }

        .dot {
            width: 10px;
            height: 10px;
            border-radius: 50%;
            background: var(--neon-cyan);
            box-shadow: 0 0 10px var(--neon-cyan);
            animation: pulse-green 2s infinite;
        }

        @keyframes pulse-green {
            0%, 100% { transform: scale(1); opacity: 1; }
            50% { transform: scale(1.3); opacity: 0.6; }
        }

        /* Top Metrics */
        .stats-bar {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
            gap: 14px;
            margin-bottom: 20px;
        }

        .stat-item {
            background: var(--card-bg);
            border: 1px solid var(--border-color);
            border-radius: 12px;
            padding: 16px 20px;
            display: flex;
            flex-direction: column;
            gap: 4px;
        }

        .stat-label {
            font-size: 12px;
            font-weight: 700;
            color: var(--text-muted);
            text-transform: uppercase;
        }

        .stat-val {
            font-family: 'JetBrains Mono', monospace;
            font-size: 24px;
            font-weight: 900;
            color: var(--neon-cyan);
        }

        .stat-val.yellow { color: var(--neon-yellow); }
        .stat-val.pink { color: var(--neon-pink); }
        .stat-val.green { color: var(--neon-green); }

        /* Main Workspace */
        .workspace-grid {
            display: grid;
            grid-template-columns: 360px 1fr;
            gap: 20px;
        }

        @media (max-width: 900px) {
            .workspace-grid { grid-template-columns: 1fr; }
        }

        .card {
            background: var(--card-bg);
            border: 1px solid var(--border-color);
            border-radius: 16px;
            padding: 20px;
            display: flex;
            flex-direction: column;
        }

        .card-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 16px;
            border-bottom: 1px solid rgba(255, 255, 255, 0.08);
            padding-bottom: 10px;
        }

        .card-header h2 {
            font-size: 17px;
            font-weight: 800;
            color: var(--neon-cyan);
            display: flex;
            align-items: center;
            gap: 8px;
        }

        /* Controls Panel */
        .form-group {
            margin-bottom: 16px;
        }

        .form-group label {
            display: flex;
            justify-content: space-between;
            font-size: 13px;
            font-weight: 700;
            margin-bottom: 6px;
        }

        .form-group label span {
            color: var(--neon-yellow);
            font-family: 'JetBrains Mono', monospace;
        }

        input[type="range"] {
            width: 100%;
            accent-color: var(--neon-cyan);
            cursor: pointer;
        }

        textarea, input[type="text"] {
            width: 100%;
            background: rgba(0, 0, 0, 0.4);
            border: 1px solid rgba(255, 255, 255, 0.15);
            padding: 10px 14px;
            border-radius: 8px;
            color: #fff;
            font-family: 'JetBrains Mono', monospace;
            font-size: 13px;
            outline: none;
        }

        textarea {
            resize: none;
            height: 70px;
        }

        textarea:focus, input[type="text"]:focus {
            border-color: var(--neon-cyan);
        }

        .btn {
            padding: 12px 18px;
            border-radius: 10px;
            font-size: 14px;
            font-weight: 800;
            border: none;
            cursor: pointer;
            transition: all 0.2s;
            display: flex;
            align-items: center;
            justify-content: center;
            gap: 8px;
        }

        .btn-primary {
            background: linear-gradient(135deg, var(--neon-cyan), var(--neon-blue));
            color: #050608;
            width: 100%;
        }

        .btn-primary:hover {
            transform: translateY(-2px);
            box-shadow: 0 4px 20px rgba(0, 255, 204, 0.4);
        }

        .btn-stress {
            background: linear-gradient(135deg, #ff0055, #ff5500);
            color: #fff;
            width: 100%;
            margin-top: 10px;
        }

        .btn-stress.running {
            animation: pulse-danger 1s infinite alternate;
        }

        @keyframes pulse-danger {
            from { box-shadow: 0 0 10px rgba(255, 0, 85, 0.5); }
            to { box-shadow: 0 0 25px rgba(255, 0, 85, 0.9); }
        }

        .btn-clear {
            background: rgba(255, 255, 255, 0.08);
            color: var(--text-muted);
            padding: 6px 12px;
            font-size: 12px;
            border-radius: 6px;
        }

        .btn-clear:hover {
            background: rgba(255, 255, 255, 0.15);
            color: #fff;
        }

        /* Live Stream Terminal Box */
        .stream-card {
            display: flex;
            flex-direction: column;
            height: 600px;
        }

        .stream-tools {
            display: flex;
            align-items: center;
            gap: 10px;
        }

        .stream-box {
            flex: 1;
            background: #05060a;
            border: 1px solid rgba(255, 255, 255, 0.08);
            border-radius: 12px;
            padding: 16px;
            overflow-y: auto;
            font-family: 'JetBrains Mono', monospace;
            font-size: 13px;
            line-height: 1.6;
            display: flex;
            flex-direction: column;
            gap: 6px;
        }

        .msg-row {
            display: flex;
            align-items: flex-start;
            gap: 10px;
            padding: 6px 10px;
            background: rgba(255, 255, 255, 0.02);
            border-radius: 6px;
            border-left: 3px solid var(--neon-cyan);
            animation: fadeIn 0.2s ease;
            word-break: break-all;
        }

        @keyframes fadeIn {
            from { opacity: 0; transform: translateX(-5px); }
            to { opacity: 1; transform: translateX(0); }
        }

        .msg-row.self {
            border-left-color: var(--neon-yellow);
            background: rgba(255, 230, 0, 0.04);
        }

        .msg-row.system {
            border-left-color: var(--neon-blue);
            background: rgba(0, 180, 216, 0.04);
        }

        .msg-row.stress {
            border-left-color: var(--neon-pink);
            background: rgba(255, 0, 85, 0.04);
        }

        .msg-time {
            color: var(--text-muted);
            font-size: 11px;
            min-width: 85px;
        }

        .msg-sender {
            color: var(--neon-cyan);
            font-weight: 700;
            min-width: 90px;
        }

        .msg-row.self .msg-sender { color: var(--neon-yellow); }
        .msg-row.system .msg-sender { color: var(--neon-blue); }
        .msg-row.stress .msg-sender { color: var(--neon-pink); }

        .msg-content {
            color: #e2e8f0;
            flex: 1;
        }

        .badge-tag {
            font-size: 10px;
            padding: 2px 6px;
            border-radius: 4px;
            background: rgba(0, 255, 204, 0.15);
            color: var(--neon-cyan);
            margin-right: 6px;
        }
    </style>
</head>
<body>
    <div class="container">
        <!-- Header -->
        <header>
            <div class="header-title">
                <h1>📡 LIVE REAL-TIME WEBSOCKET STREAM</h1>
                <span>Direct Multi-Peer Real-Time Data Broadcaster</span>
            </div>
            <div class="peer-badge">
                <span class="dot" id="statusDot"></span>
                <span id="myPeerId">Connecting...</span>
                <span>|</span>
                <span>👥 Online: <strong id="valOnline" style="color:var(--neon-cyan)">1</strong></span>
            </div>
        </header>

        <!-- Stats Bar -->
        <div class="stats-bar">
            <div class="stat-item">
                <span class="stat-label">Live Incoming Messages</span>
                <span class="stat-val" id="valReceivedCount">0</span>
            </div>
            <div class="stat-item">
                <span class="stat-label">Messages / Sec (Rate)</span>
                <span class="stat-val yellow" id="valLiveSpeed">0 msg/s</span>
            </div>
            <div class="stat-item">
                <span class="stat-label">Total Sent</span>
                <span class="stat-val pink" id="valSentCount">0</span>
            </div>
            <div class="stat-item">
                <span class="stat-label">Server Total Processed</span>
                <span class="stat-val green" id="valServerTotal">0</span>
            </div>
        </div>

        <!-- Main Workspace -->
        <div class="workspace-grid">
            <!-- Left: Message Sender & Stress Controls -->
            <div class="card">
                <div class="card-header">
                    <h2>💬 REAL-TIME SENDER</h2>
                </div>

                <!-- Instant Message Send -->
                <div class="form-group">
                    <label>Instant Message / Payload</label>
                    <textarea id="txtManual" placeholder="Type message or JSON to broadcast... (e.g. Hello from Peer!)">Hello Real-Time WebSocket!</textarea>
                </div>
                <button class="btn btn-primary" id="btnSend">
                    📤 BROADCAST NOW (Enter)
                </button>

                <hr style="border:none; border-top:1px solid rgba(255,255,255,0.08); margin: 20px 0;">

                <!-- Stress Flood Controls -->
                <div class="card-header" style="border:none; padding:0; margin-bottom:12px;">
                    <h2>🔥 AUTO STRESS FLOODER</h2>
                </div>

                <div class="form-group">
                    <label>
                        Sending Speed (Delay)
                        <span id="txtDelay">10 ms (~100 msg/s)</span>
                    </label>
                    <input type="range" id="rngDelay" min="2" max="200" value="10">
                </div>

                <button class="btn btn-stress" id="btnStress">
                    🚀 START AUTO FLOOD (টানা পাঠাতে থাকো)
                </button>
            </div>

            <!-- Right: Real-time Live Stream View -->
            <div class="card stream-card">
                <div class="card-header">
                    <h2>
                        📺 LIVE REAL-TIME FEED
                        <span style="font-size:12px; font-weight:normal; color:var(--text-muted);">(প্রতিটি মেসেজ সরাসরি রেন্ডার হচ্ছে)</span>
                    </h2>
                    <div class="stream-tools">
                        <label style="font-size:12px; color:var(--text-muted); display:flex; align-items:center; gap:4px; cursor:pointer;">
                            <input type="checkbox" id="chkAutoScroll" checked> Auto-Scroll
                        </label>
                        <button class="btn btn-clear" id="btnClear">Clear Stream</button>
                    </div>
                </div>

                <!-- Stream Box -->
                <div class="stream-box" id="streamBox">
                    <div class="msg-row system">
                        <span class="msg-time">[SYSTEM]</span>
                        <span class="msg-sender">SYSTEM</span>
                        <span class="msg-content">WebSocket stream initialized. Connected to ws://localhost:8080/ws</span>
                    </div>
                </div>
            </div>
        </div>
    </div>

    <script>
        const streamBox = document.getElementById('streamBox');
        const myPeerId = document.getElementById('myPeerId');
        const statusDot = document.getElementById('statusDot');
        const valOnline = document.getElementById('valOnline');
        const valReceivedCount = document.getElementById('valReceivedCount');
        const valLiveSpeed = document.getElementById('valLiveSpeed');
        const valSentCount = document.getElementById('valSentCount');
        const valServerTotal = document.getElementById('valServerTotal');

        const txtManual = document.getElementById('txtManual');
        const btnSend = document.getElementById('btnSend');
        const rngDelay = document.getElementById('rngDelay');
        const txtDelay = document.getElementById('txtDelay');
        const btnStress = document.getElementById('btnStress');
        const btnClear = document.getElementById('btnClear');
        const chkAutoScroll = document.getElementById('chkAutoScroll');

        let ws = null;
        let myClientId = null;
        let receivedCount = 0;
        let sentCount = 0;
        let lastReceived = 0;
        let isStressing = false;
        let stressTimer = null;
        let stressSeq = 0;

        // WebSocket Connection
        function connect() {
            const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
            ws = new WebSocket(proto + '//' + location.host + '/ws');

            ws.onopen = () => {
                statusDot.style.background = '#00ffcc';
                myPeerId.textContent = 'Connecting Handshake...';
            };

            ws.onmessage = (event) => {
                receivedCount++;
                valReceivedCount.textContent = receivedCount.toLocaleString();

                try {
                    const data = JSON.parse(event.data);
                    renderMessage(data);
                } catch (e) {
                    renderRawMessage(event.data);
                }
            };

            ws.onclose = () => {
                statusDot.style.background = '#ff0055';
                myPeerId.textContent = 'Disconnected (Reconnecting...)';
                setTimeout(connect, 2000);
            };
        }

        // Render formatted message directly onto stream
        function renderMessage(msg) {
            if (msg.type === 'welcome') {
                myClientId = msg.clientId;
                myPeerId.textContent = 'ID: ' + myClientId;
                return;
            }

            if (msg.type === 'system') {
                if (msg.online !== undefined) {
                    valOnline.textContent = msg.online;
                }
                appendRow('system', msg.timestamp || getTimestamp(), 'SYSTEM', msg.message);
                return;
            }

            if (msg.type === 'message') {
                const isMe = (msg.sender === myClientId);
                const isStress = msg.payload && msg.payload.includes('STRESS');
                const rowType = isMe ? 'self' : (isStress ? 'stress' : 'peer');
                appendRow(rowType, msg.timestamp || getTimestamp(), msg.sender || 'PEER', msg.payload);
            }
        }

        function renderRawMessage(raw) {
            appendRow('peer', getTimestamp(), 'RAW', raw);
        }

        function appendRow(type, time, sender, content) {
            const row = document.createElement('div');
            row.className = 'msg-row ' + type;

            let tag = '';
            if (type === 'self') tag = '<span class="badge-tag" style="background:#ffe60022; color:#ffe600;">YOU</span>';
            else if (type === 'stress') tag = '<span class="badge-tag" style="background:#ff005522; color:#ff0055;">BURST</span>';

            row.innerHTML = 
                '<span class="msg-time">[' + time + ']</span>' +
                '<span class="msg-sender">' + tag + escapeHtml(sender) + '</span>' +
                '<span class="msg-content">' + escapeHtml(content) + '</span>';

            streamBox.appendChild(row);

            // Limit DOM elements to keep browser fast during flood
            if (streamBox.children.length > 300) {
                streamBox.removeChild(streamBox.firstChild);
            }

            if (chkAutoScroll.checked) {
                streamBox.scrollTop = streamBox.scrollHeight;
            }
        }

        function getTimestamp() {
            const d = new Date();
            return d.toTimeString().split(' ')[0] + '.' + String(d.getMilliseconds()).padStart(3, '0');
        }

        function escapeHtml(str) {
            if (typeof str !== 'string') str = JSON.stringify(str);
            const div = document.createElement('div');
            div.innerText = str;
            return div.innerHTML;
        }

        // Send manual message
        function sendManual() {
            const val = txtManual.value.trim();
            if (val && ws && ws.readyState === WebSocket.OPEN) {
                ws.send(val);
                sentCount++;
                valSentCount.textContent = sentCount.toLocaleString();
            }
        }

        btnSend.addEventListener('click', sendManual);
        txtManual.addEventListener('keydown', (e) => {
            if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault();
                sendManual();
            }
        });

        // Stress Flooder
        rngDelay.addEventListener('input', () => {
            const delay = parseInt(rngDelay.value);
            txtDelay.textContent = delay + ' ms (~' + Math.round(1000 / delay) + ' msg/s)';
            if (isStressing) {
                startStressTimer();
            }
        });

        btnStress.addEventListener('click', () => {
            if (!isStressing) {
                isStressing = true;
                btnStress.textContent = '🛑 STOP AUTO FLOOD (বন্ধ করো)';
                btnStress.classList.add('running');
                startStressTimer();
            } else {
                isStressing = false;
                btnStress.textContent = '🚀 START AUTO FLOOD (টানা পাঠাতে থাকো)';
                btnStress.classList.remove('running');
                if (stressTimer) clearInterval(stressTimer);
            }
        });

        function startStressTimer() {
            if (stressTimer) clearInterval(stressTimer);
            const delay = parseInt(rngDelay.value);
            stressTimer = setInterval(() => {
                if (!isStressing || !ws || ws.readyState !== WebSocket.OPEN) return;
                stressSeq++;
                const payload = 'STRESS_DATA_#' + stressSeq + ' | Time: ' + getTimestamp() + ' | Random: ' + Math.floor(Math.random() * 10000);
                ws.send(payload);
                sentCount++;
                valSentCount.textContent = sentCount.toLocaleString();
            }, delay);
        }

        btnClear.addEventListener('click', () => {
            streamBox.innerHTML = '';
        });

        // Speed calculation ticker
        setInterval(async () => {
            const diff = receivedCount - lastReceived;
            lastReceived = receivedCount;
            valLiveSpeed.textContent = diff.toLocaleString() + ' msg/s';

            try {
                const res = await fetch('/api/stats');
                const stats = await res.json();
                valServerTotal.textContent = (stats.totalReceived || 0).toLocaleString();
                if (stats.activeClients) valOnline.textContent = stats.activeClients;
            } catch(e) {}
        }, 1000);

        connect();
    </script>
</body>
</html>
`
