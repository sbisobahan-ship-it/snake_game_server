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
	ReadBufferSize:  1024 * 1024,
	WriteBufferSize: 1024 * 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
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
		broadcast:  make(chan []byte, 2000),
		register:   make(chan *Client, 128),
		unregister: make(chan *Client, 128),
	}

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

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.Send)
			}
			h.mu.Unlock()

		case message := <-h.broadcast:
			msgLen := uint64(len(message))
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.Send <- message:
					atomic.AddUint64(&h.totalSent, 1)
					atomic.AddUint64(&h.totalBytesSent, msgLen)
				default:
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

	c.Conn.SetReadLimit(10 * 1024 * 1024)

	for {
		_, raw, err := c.Conn.ReadMessage()
		if err != nil {
			break
		}
		rawLen := uint64(len(raw))
		atomic.AddUint64(&h.totalReceived, 1)
		atomic.AddUint64(&h.totalBytesRecv, rawLen)

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

	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		clientID := fmt.Sprintf("PEER-%04d", time.Now().UnixNano()%10000)
		client := &Client{
			ID:   clientID,
			Conn: conn,
			Send: make(chan []byte, 100),
		}

		hub.register <- client

		go client.writePump()
		go client.readPump(hub)
	})

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

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(htmlScrollingTerminalFlooder))
	})

	addr := ":" + port
	fmt.Printf("==============================================================\n")
	fmt.Printf("⚡ Live Rolling Terminal Flooder Server Online!\n")
	fmt.Printf("🌐 Rolling Terminal Page     : http://localhost:%s\n", port)
	fmt.Printf("📡 WebSocket Stream Endpoint : ws://localhost:%s/ws\n", port)
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

const htmlScrollingTerminalFlooder = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>1MB Live Rolling Terminal Stream</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            background-color: #000000;
            color: #00ff66;
            font-family: 'Consolas', 'Courier New', monospace;
            height: 100vh;
            display: flex;
            flex-direction: column;
            padding: 10px 14px;
            overflow: hidden;
            user-select: none;
        }

        /* Top HUD Bar */
        .hud-bar {
            display: flex;
            justify-content: space-between;
            align-items: center;
            background: #050505;
            border: 1px solid #1a2a1a;
            border-bottom: 2px solid #00ff66;
            padding: 10px 18px;
            font-size: 14px;
            margin-bottom: 8px;
            flex-shrink: 0;
        }

        .hud-left {
            display: flex;
            align-items: center;
            gap: 12px;
        }

        .hud-dot {
            width: 12px;
            height: 12px;
            border-radius: 50%;
            background: #ff0055;
            box-shadow: 0 0 14px #ff0055;
            animation: blink 0.25s infinite alternate;
        }

        @keyframes blink {
            from { opacity: 0.3; }
            to { opacity: 1; }
        }

        .hud-speed {
            color: #ffffff;
            font-weight: 900;
            font-size: 20px;
            text-shadow: 0 0 10px #00ff66;
        }

        .hud-metrics {
            display: flex;
            gap: 20px;
            font-size: 13px;
        }

        .metric-tag span { color: #667788; }
        .metric-tag strong { color: #00ffcc; margin-left: 4px; }
        .metric-tag strong.tx { color: #ff0055; }
        .metric-tag strong.rx { color: #ffe600; }

        /* Rolling Terminal Container */
        .terminal-viewport {
            flex: 1;
            background: #000000;
            border: 1px solid #112211;
            padding: 10px 14px;
            overflow: hidden;
            display: flex;
            flex-direction: column;
            justify-content: flex-end; /* Keeps items pushed upwards */
            box-shadow: inset 0 0 50px rgba(0, 255, 102, 0.04);
        }

        .stream-list {
            display: flex;
            flex-direction: column;
            gap: 4px;
        }

        /* Terminal Row Styles */
        .t-row {
            display: flex;
            align-items: center;
            gap: 10px;
            font-size: 13.5px;
            line-height: 1.4;
            white-space: nowrap;
            overflow: hidden;
            animation: slideUp 0.1s ease-out;
        }

        @keyframes slideUp {
            from { opacity: 0; transform: translateY(8px); }
            to { opacity: 1; transform: translateY(0); }
        }

        .t-time {
            color: #556677;
            font-size: 12px;
            min-width: 95px;
        }

        .t-badge {
            padding: 2px 6px;
            border-radius: 3px;
            font-size: 11px;
            font-weight: 900;
            min-width: 100px;
            text-align: center;
        }

        .t-badge.tx {
            background: rgba(255, 0, 85, 0.2);
            color: #ff0055;
            border: 1px solid rgba(255, 0, 85, 0.4);
        }

        .t-badge.rx {
            background: rgba(255, 230, 0, 0.2);
            color: #ffe600;
            border: 1px solid rgba(255, 230, 0, 0.4);
        }

        .t-badge.sys {
            background: rgba(0, 180, 216, 0.2);
            color: #00b4d8;
            border: 1px solid rgba(0, 180, 216, 0.4);
        }

        .t-bytes {
            color: #00ffcc;
            font-weight: bold;
            min-width: 135px;
        }

        .t-hex {
            color: #88bb88;
            font-weight: 500;
        }

        .t-chunk {
            color: #667799;
            font-size: 12px;
        }

        /* Footer */
        .footer-bar {
            margin-top: 6px;
            font-size: 11px;
            color: #334455;
            display: flex;
            justify-content: space-between;
            padding: 0 4px;
            flex-shrink: 0;
        }
    </style>
</head>
<body>
    <!-- Top HUD Telemetry -->
    <div class="hud-bar">
        <div class="hud-left">
            <div class="hud-dot"></div>
            <span style="color:#ff0055; font-weight:bold;">AUTO 1MB ROLLING STREAM</span>
            <span>|</span>
            <span class="hud-speed" id="txtSpeed">0.00 MB/s</span>
        </div>

        <div class="hud-metrics">
            <div class="metric-tag"><span>TX SENT:</span><strong class="tx" id="txtSent">0 MB</strong></div>
            <div class="metric-tag"><span>RX RECV:</span><strong class="rx" id="txtRecv">0 MB</strong></div>
            <div class="metric-tag"><span>PEERS:</span><strong id="txtPeers">1</strong></div>
            <div class="metric-tag"><span>SERVER TOTAL:</span><strong id="txtServerTotal">0 MB</strong></div>
        </div>
    </div>

    <!-- Live Rolling Stream -->
    <div class="terminal-viewport">
        <div class="stream-list" id="streamList">
            <div class="t-row">
                <span class="t-time">[INIT]</span>
                <span class="t-badge sys">SYSTEM_INIT</span>
                <span class="t-bytes">1,048,576 BYTES</span>
                <span class="t-hex">57 45 42 53 4F 43 4B 45 54 5F 31 4D 42 5F 53 54 52 45 41 4D</span>
                <span class="t-chunk">STATUS: INITIALIZED & READY</span>
            </div>
        </div>
    </div>

    <div class="footer-bar">
        <span>FRAME: 1,048,576 BYTES (1.00 MB) | MILLISECOND AVALANCHE | AUTO SLIDE & PURGE</span>
        <span>STREAM: NO-CACHE ROLLING ACTIVE</span>
    </div>

    <script>
        const streamList = document.getElementById('streamList');
        const txtSpeed = document.getElementById('txtSpeed');
        const txtSent = document.getElementById('txtSent');
        const txtRecv = document.getElementById('txtRecv');
        const txtPeers = document.getElementById('txtPeers');
        const txtServerTotal = document.getElementById('txtServerTotal');

        let totalSentBytes = 0;
        let totalRecvBytes = 0;
        let lastSentBytes = 0;
        let lastRecvBytes = 0;
        let txSeq = 0;
        let rxSeq = 0;

        const MAX_LINES = 28; // Keep only latest 28 lines, older slide up & remove instantly

        const ONE_MB = 1024 * 1024;
        const oneMbBuffer = new Uint8Array(ONE_MB);
        for (let i = 0; i < ONE_MB; i++) {
            oneMbBuffer[i] = (i % 256);
        }

        function getTime() {
            const d = new Date();
            return d.toTimeString().split(' ')[0] + '.' + String(d.getMilliseconds()).padStart(3, '0');
        }

        function generateRandomHex() {
            let h = '';
            for (let i = 0; i < 16; i++) {
                h += Math.floor(Math.random() * 256).toString(16).padStart(2, '0').toUpperCase() + ' ';
            }
            return h.trim();
        }

        function addRow(badgeType, badgeText, hexText, chunkText) {
            const row = document.createElement('div');
            row.className = 't-row';
            row.innerHTML = 
                '<span class="t-time">[' + getTime() + ']</span>' +
                '<span class="t-badge ' + badgeType + '">' + badgeText + '</span>' +
                '<span class="t-bytes">1,048,576 BYTES</span>' +
                '<span class="t-hex">' + hexText + '</span>' +
                '<span class="t-chunk">' + chunkText + '</span>';

            streamList.appendChild(row);

            // Remove oldest line from top so it slides up and vanishes cleanly without caching
            while (streamList.children.length > MAX_LINES) {
                streamList.removeChild(streamList.firstChild);
            }
        }

        const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = proto + '//' + location.host + '/ws';
        let ws = null;

        function connect() {
            ws = new WebSocket(wsUrl);
            ws.binaryType = 'arraybuffer';

            ws.onopen = () => {
                addRow('sys', 'WS_CONNECTED', generateRandomHex(), 'ESTABLISHED 1MB BURST STREAM');
                floodLoop();
            };

            let rxThrottle = 0;
            ws.onmessage = (event) => {
                const len = event.data.byteLength || event.data.length || ONE_MB;
                totalRecvBytes += len;
                rxSeq++;

                rxThrottle++;
                if (rxThrottle % 2 === 0) {
                    addRow('rx', 'RX ◀ PEER', generateRandomHex(), 'CHUNK #' + rxSeq + ' [OK]');
                }
            };

            ws.onclose = () => {
                addRow('sys', 'DISCONNECTED', generateRandomHex(), 'RETRYING CONNECT...');
                setTimeout(connect, 1000);
            };

            ws.onerror = () => {};
        }

        let txThrottle = 0;
        function floodLoop() {
            if (!ws || ws.readyState !== WebSocket.OPEN) return;

            if (ws.bufferedAmount < 4 * ONE_MB) {
                ws.send(oneMbBuffer);
                totalSentBytes += ONE_MB;
                txSeq++;

                txThrottle++;
                if (txThrottle % 2 === 0) {
                    addRow('tx', 'TX ▶ SERVER', generateRandomHex(), 'BLOCK #' + txSeq + ' [SENT]');
                }
            }

            setTimeout(floodLoop, 1);
        }

        // Stats updater
        setInterval(async () => {
            const diffSent = totalSentBytes - lastSentBytes;
            const diffRecv = totalRecvBytes - lastRecvBytes;
            lastSentBytes = totalSentBytes;
            lastRecvBytes = totalRecvBytes;

            const totalSpeed = ((diffSent + diffRecv) / (1024 * 1024)).toFixed(2);
            txtSpeed.textContent = totalSpeed + ' MB/s';

            txtSent.textContent = (totalSentBytes / (1024 * 1024)).toFixed(0) + ' MB';
            txtRecv.textContent = (totalRecvBytes / (1024 * 1024)).toFixed(0) + ' MB';

            try {
                const res = await fetch('/api/stats');
                const data = await res.json();
                if (data.activeClients !== undefined) txtPeers.textContent = data.activeClients;
                if (data.totalMBRecv) txtServerTotal.textContent = data.totalMBRecv;
            } catch(e) {}
        }, 1000);

        connect();
    </script>
</body>
</html>
`
