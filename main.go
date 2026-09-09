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
	ReadBufferSize:  256 * 1024,
	WriteBufferSize: 256 * 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type PacketMessage struct {
	SenderID string
	Payload  []byte
}

type Client struct {
	ID   string
	Hub  *Hub
	Conn *websocket.Conn
	Send chan []byte
}

type Hub struct {
	clients        map[*Client]bool
	broadcast      chan PacketMessage
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
		broadcast:  make(chan PacketMessage, 5000),
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
			total := len(h.clients)
			h.mu.Unlock()
			log.Printf("🔥 [Connected] %s | Active Clients: %d", client.ID, total)

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.Send)
			}
			total := len(h.clients)
			h.mu.Unlock()
			log.Printf("🛑 [Disconnected] %s | Active Clients: %d", client.ID, total)

		case pkt := <-h.broadcast:
			msgLen := uint64(len(pkt.Payload))
			h.mu.RLock()
			for client := range h.clients {
				// Broadcast to ALL other peers (and even to self if only 1 client connected so they see response)
				if client.ID != pkt.SenderID || len(h.clients) == 1 {
					select {
					case client.Send <- pkt.Payload:
						atomic.AddUint64(&h.totalSent, 1)
						atomic.AddUint64(&h.totalBytesSent, msgLen)
					default:
					}
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

func (c *Client) readPump() {
	defer func() {
		c.Hub.unregister <- c
		c.Conn.Close()
	}()

	c.Conn.SetReadLimit(10 * 1024 * 1024)

	for {
		_, raw, err := c.Conn.ReadMessage()
		if err != nil {
			break
		}
		rawLen := uint64(len(raw))
		atomic.AddUint64(&c.Hub.totalReceived, 1)
		atomic.AddUint64(&c.Hub.totalBytesRecv, rawLen)

		// Broadcast packet to other peers
		select {
		case c.Hub.broadcast <- PacketMessage{SenderID: c.ID, Payload: raw}:
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
			Hub:  hub,
			Conn: conn,
			Send: make(chan []byte, 500),
		}

		hub.register <- client

		go client.writePump()
		go client.readPump()
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
		w.Write([]byte(htmlRealTimePeerStream))
	})

	addr := ":" + port
	fmt.Printf("==============================================================\n")
	fmt.Printf("⚡ Multi-Peer WebSocket Real-Time Relay Server Online!\n")
	fmt.Printf("🌐 Peer Stream Dashboard    : http://localhost:%s\n", port)
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

const htmlRealTimePeerStream = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>⚡ Live Multi-Peer WebSocket Data Stream</title>
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

        /* Top HUD */
        .hud-bar {
            display: flex;
            justify-content: space-between;
            align-items: center;
            background: #060608;
            border: 1px solid #142214;
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
            background: #00ffcc;
            box-shadow: 0 0 14px #00ffcc;
            animation: blink 0.4s infinite alternate;
        }

        .hud-dot.active {
            background: #ff0055;
            box-shadow: 0 0 16px #ff0055;
        }

        @keyframes blink {
            from { opacity: 0.3; transform: scale(0.9); }
            to { opacity: 1; transform: scale(1.2); }
        }

        .hud-speed {
            color: #ffffff;
            font-weight: 900;
            font-size: 20px;
            text-shadow: 0 0 10px #00ff66;
        }

        .hud-metrics {
            display: flex;
            gap: 24px;
            font-size: 13.5px;
        }

        .metric-tag span { color: #667788; }
        .metric-tag strong { color: #00ffcc; margin-left: 4px; }
        .metric-tag strong.tx { color: #ff0055; }
        .metric-tag strong.rx { color: #ffe600; }

        /* Terminal Screen */
        .terminal-viewport {
            flex: 1;
            background: #020204;
            border: 1px solid #0f1c0f;
            padding: 10px 14px;
            overflow: hidden;
            display: flex;
            flex-direction: column;
            justify-content: flex-end;
            box-shadow: inset 0 0 50px rgba(0, 255, 102, 0.03);
        }

        .stream-list {
            display: flex;
            flex-direction: column;
            gap: 4px;
        }

        .t-row {
            display: flex;
            align-items: center;
            gap: 10px;
            font-size: 13.5px;
            line-height: 1.4;
            white-space: nowrap;
            overflow: hidden;
            animation: slideUp 0.08s ease-out;
        }

        @keyframes slideUp {
            from { opacity: 0; transform: translateY(6px); }
            to { opacity: 1; transform: translateY(0); }
        }

        .t-time {
            color: #556677;
            font-size: 12px;
            min-width: 95px;
        }

        .t-badge {
            padding: 2px 8px;
            border-radius: 3px;
            font-size: 11px;
            font-weight: 900;
            min-width: 110px;
            text-align: center;
        }

        .t-badge.tx {
            background: rgba(255, 0, 85, 0.2);
            color: #ff0055;
            border: 1px solid rgba(255, 0, 85, 0.5);
        }

        .t-badge.rx {
            background: rgba(255, 230, 0, 0.2);
            color: #ffe600;
            border: 1px solid rgba(255, 230, 0, 0.5);
        }

        .t-badge.sys {
            background: rgba(0, 180, 216, 0.2);
            color: #00b4d8;
            border: 1px solid rgba(0, 180, 216, 0.5);
        }

        .t-bytes {
            color: #00ffcc;
            font-weight: bold;
            min-width: 120px;
        }

        .t-hex {
            color: #77dd77;
            font-weight: 500;
        }

        .t-chunk {
            color: #8899bb;
            font-size: 12px;
        }

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
    <div class="hud-bar">
        <div class="hud-left">
            <div class="hud-dot active" id="dotStatus"></div>
            <span style="color:#ff0055; font-weight:bold;">LIVE PEER STREAM</span>
            <span>|</span>
            <span class="hud-speed" id="txtSpeed">0.00 MB/s</span>
        </div>

        <div class="hud-metrics">
            <div class="metric-tag"><span>MY PEER ID:</span><strong id="txtMyPeer">PEER-INIT</strong></div>
            <div class="metric-tag"><span>TX SENT:</span><strong class="tx" id="txtSent">0.0 MB</strong></div>
            <div class="metric-tag"><span>RX RECV:</span><strong class="rx" id="txtRecv">0.0 MB</strong></div>
            <div class="metric-tag"><span>ONLINE PEERS:</span><strong id="txtPeers">1</strong></div>
        </div>
    </div>

    <!-- Live Terminal -->
    <div class="terminal-viewport">
        <div class="stream-list" id="streamList">
            <div class="t-row">
                <span class="t-time">[INIT]</span>
                <span class="t-badge sys">SYS_CONNECTED</span>
                <span class="t-bytes">65,536 BYTES</span>
                <span class="t-hex">57 53 5F 53 54 52 45 41 4D 5F 4C 49 56 45 5F 4F 4E 4C 49 4E 45</span>
                <span class="t-chunk">REAL-TIME TWO-WAY DATA RELAY READY</span>
            </div>
        </div>
    </div>

    <div class="footer-bar">
        <span>FRAME: 65,536 BYTES (64 KB BURST) | CONTINUOUS MILLISECOND LOOP | FULL P2P RELAY</span>
        <span>STATUS: LIVE TRANSMITTING & RECEIVING</span>
    </div>

    <script>
        const streamList = document.getElementById('streamList');
        const txtSpeed = document.getElementById('txtSpeed');
        const txtMyPeer = document.getElementById('txtMyPeer');
        const txtSent = document.getElementById('txtSent');
        const txtRecv = document.getElementById('txtRecv');
        const txtPeers = document.getElementById('txtPeers');

        let totalSentBytes = 0;
        let totalRecvBytes = 0;
        let lastSentBytes = 0;
        let lastRecvBytes = 0;
        let txSeq = 0;
        let rxSeq = 0;

        const MAX_LINES = 28;

        // 64 KB Frame chunk size (ideal balance: high MB/s speed without freezing browser UI)
        const FRAME_SIZE = 64 * 1024;
        const myPeerId = 'PEER-' + Math.floor(1000 + Math.random() * 9000);
        txtMyPeer.textContent = myPeerId;

        // Prepare frame with sender ID tag
        const frameHeader = new TextEncoder().encode('SENDER:' + myPeerId + '|');
        const frameBuffer = new Uint8Array(FRAME_SIZE);
        frameBuffer.set(frameHeader, 0);
        for (let i = frameHeader.length; i < FRAME_SIZE; i++) {
            frameBuffer[i] = (i % 256);
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
                '<span class="t-bytes">65,536 BYTES</span>' +
                '<span class="t-hex">' + hexText + '</span>' +
                '<span class="t-chunk">' + chunkText + '</span>';

            streamList.appendChild(row);

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
                addRow('sys', 'WS_RELAY_ON', generateRandomHex(), 'CONNECT OK | AUTO BROADCAST STREAMING');
                floodLoop();
            };

            ws.onmessage = (event) => {
                const len = event.data.byteLength || event.data.length || FRAME_SIZE;
                totalRecvBytes += len;
                rxSeq++;

                // Extract peer tag if available
                let fromPeer = 'REMOTE_PEER';
                try {
                    const strHeader = new TextDecoder().decode(new Uint8Array(event.data.slice(0, 24)));
                    if (strHeader.startsWith('SENDER:')) {
                        fromPeer = strHeader.split('|')[0].replace('SENDER:', '');
                    }
                } catch(e) {}

                if (rxSeq % 2 === 0) {
                    addRow('rx', 'RX ◀ ' + fromPeer, generateRandomHex(), 'FRAME #' + rxSeq + ' [INGESTED]');
                }
            };

            ws.onclose = () => {
                addRow('sys', 'DISCONNECTED', generateRandomHex(), 'RETRYING CONNECT IN 1s...');
                setTimeout(connect, 1000);
            };

            ws.onerror = () => {};
        }

        function floodLoop() {
            if (!ws || ws.readyState !== WebSocket.OPEN) return;

            // Send 64KB frame
            if (ws.bufferedAmount < 512 * 1024) {
                ws.send(frameBuffer);
                totalSentBytes += FRAME_SIZE;
                txSeq++;

                if (txSeq % 2 === 0) {
                    addRow('tx', 'TX ▶ TO PEERS', generateRandomHex(), 'FRAME #' + txSeq + ' [TRANSMITTED]');
                }
            }

            // Continuous loop
            setTimeout(floodLoop, 10);
        }

        // Stats updater
        setInterval(async () => {
            const diffSent = totalSentBytes - lastSentBytes;
            const diffRecv = totalRecvBytes - lastRecvBytes;
            lastSentBytes = totalSentBytes;
            lastRecvBytes = totalRecvBytes;

            const totalSpeed = ((diffSent + diffRecv) / (1024 * 1024)).toFixed(2);
            txtSpeed.textContent = totalSpeed + ' MB/s';

            txtSent.textContent = (totalSentBytes / (1024 * 1024)).toFixed(1) + ' MB';
            txtRecv.textContent = (totalRecvBytes / (1024 * 1024)).toFixed(1) + ' MB';

            try {
                const res = await fetch('/api/stats');
                const data = await res.json();
                if (data.activeClients !== undefined) txtPeers.textContent = data.activeClients;
            } catch(e) {}
        }, 1000);

        connect();
    </script>
</body>
</html>
`
