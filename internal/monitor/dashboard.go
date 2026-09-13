package monitor

import (
	"net/http"
)

const DashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Snake Server - Real-Time Multi-Terminal Telemetry</title>
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
    <link href="https://fonts.googleapis.com/css2?family=Fira+Code:wght@400;500;600;700&family=Inter:wght@400;600;700;800&display=swap" rel="stylesheet">
    <style>
        :root {
            --bg-base: #070a12;
            --bg-panel: #0d1322;
            --bg-terminal: #050811;
            --border-dim: #1e293b;
            
            --neon-blue: #38bdf8;
            --neon-green: #22c55e;
            --neon-amber: #f59e0b;
            --neon-rose: #f43f5e;
            --neon-purple: #a855f7;
            --neon-cyan: #06b6d4;

            --text-main: #f8fafc;
            --text-muted: #94a3b8;
            --text-dim: #475569;
        }

        * { box-sizing: border-box; margin: 0; padding: 0; }
        
        body {
            background-color: var(--bg-base);
            background-image: 
                radial-gradient(circle at 10% 20%, rgba(56, 189, 248, 0.05) 0%, transparent 40%),
                radial-gradient(circle at 90% 80%, rgba(168, 85, 247, 0.05) 0%, transparent 40%),
                linear-gradient(rgba(30, 41, 59, 0.15) 1px, transparent 1px),
                linear-gradient(90deg, rgba(30, 41, 59, 0.15) 1px, transparent 1px);
            background-size: 100% 100%, 100% 100%, 30px 30px, 30px 30px;
            color: var(--text-main);
            font-family: 'Inter', sans-serif;
            height: 100vh;
            display: flex;
            flex-direction: column;
            overflow: hidden;
        }

        /* Top Header */
        header {
            background: rgba(13, 19, 34, 0.95);
            backdrop-filter: blur(12px);
            border-bottom: 1px solid var(--border-dim);
            padding: 8px 16px;
            display: flex;
            align-items: center;
            justify-content: space-between;
            gap: 12px;
            flex-shrink: 0;
            flex-wrap: wrap;
        }

        .brand {
            display: flex;
            align-items: center;
            gap: 10px;
        }

        .pulse-dot {
            width: 10px;
            height: 10px;
            border-radius: 50%;
            background: var(--neon-rose);
            box-shadow: 0 0 10px var(--neon-rose);
            transition: all 0.3s;
        }

        .pulse-dot.connected {
            background: var(--neon-green);
            box-shadow: 0 0 12px var(--neon-green);
            animation: pulse 1.8s infinite;
        }

        @keyframes pulse {
            0% { transform: scale(0.9); opacity: 0.8; }
            50% { transform: scale(1.2); opacity: 1; }
            100% { transform: scale(0.9); opacity: 0.8; }
        }

        .brand h1 {
            font-size: 14px;
            font-weight: 700;
            color: #fff;
            letter-spacing: -0.3px;
        }

        /* Auth Bar */
        .auth-bar {
            display: flex;
            align-items: center;
            gap: 8px;
            background: rgba(15, 23, 42, 0.9);
            padding: 4px 8px;
            border-radius: 8px;
            border: 1px solid var(--border-dim);
        }

        .auth-input {
            background: #090e1a;
            border: 1px solid #334155;
            color: #fff;
            padding: 5px 10px;
            border-radius: 5px;
            font-size: 12px;
            font-family: 'Fira Code', monospace;
            outline: none;
            transition: border-color 0.2s;
        }

        .auth-input:focus {
            border-color: var(--neon-blue);
        }

        .input-url { width: 220px; }
        .input-token { width: 110px; }

        /* Stats Bar */
        .stats-bar {
            display: flex;
            align-items: center;
            gap: 10px;
        }

        .stat-badge {
            background: rgba(15, 23, 42, 0.8);
            border: 1px solid var(--border-dim);
            padding: 4px 10px;
            border-radius: 6px;
            font-size: 11.5px;
            display: flex;
            align-items: center;
            gap: 5px;
            font-family: 'Fira Code', monospace;
        }

        .stat-badge .label { color: var(--text-muted); font-size: 10px; }
        .stat-badge .val { font-weight: 700; color: #fff; }
        .val-tps { color: var(--neon-amber) !important; }
        .val-players { color: var(--neon-green) !important; }
        .val-foods { color: var(--neon-cyan) !important; }
        .val-eaten { color: var(--neon-rose) !important; }
        .val-ram { color: var(--neon-purple) !important; }

        /* Buttons */
        .btn {
            background: #1e293b;
            color: #f1f5f9;
            border: 1px solid #334155;
            padding: 5px 12px;
            border-radius: 5px;
            font-size: 11.5px;
            font-weight: 600;
            cursor: pointer;
            transition: all 0.2s;
            display: flex;
            align-items: center;
            gap: 6px;
            font-family: 'Inter', sans-serif;
        }

        .btn:hover {
            background: #334155;
            border-color: #475569;
        }

        .btn-connect {
            background: linear-gradient(135deg, #0284c7, #2563eb);
            border-color: #38bdf8;
            color: #fff;
        }

        .btn-connect:hover {
            background: linear-gradient(135deg, #0369a1, #1d4ed8);
        }

        .btn-disconnect {
            background: rgba(244, 63, 94, 0.2);
            border-color: rgba(244, 63, 94, 0.4);
            color: #f43f5e;
        }

        .btn-disconnect:hover {
            background: rgba(244, 63, 94, 0.35);
        }

        /* 4-Terminal Grid Layout */
        .grid-container {
            flex: 1;
            display: grid;
            grid-template-columns: 1fr 1fr;
            grid-template-rows: 1fr 1fr;
            gap: 12px;
            padding: 12px;
            overflow: hidden;
        }

        @media (max-width: 960px) {
            .grid-container {
                grid-template-columns: 1fr;
                grid-template-rows: repeat(4, 1fr);
                overflow-y: auto;
            }
        }

        /* Individual Terminal Panel */
        .terminal-panel {
            background: var(--bg-terminal);
            border: 1px solid var(--border-dim);
            border-radius: 10px;
            display: flex;
            flex-direction: column;
            overflow: hidden;
            box-shadow: 0 8px 24px rgba(0, 0, 0, 0.45);
        }

        .panel-food { border-top: 3px solid var(--neon-cyan); }
        .panel-player { border-top: 3px solid var(--neon-green); }
        .panel-network { border-top: 3px solid var(--neon-blue); }
        .panel-physics { border-top: 3px solid var(--neon-purple); }

        .terminal-header {
            background: rgba(15, 23, 42, 0.95);
            padding: 8px 12px;
            display: flex;
            align-items: center;
            justify-content: space-between;
            border-bottom: 1px solid var(--border-dim);
            font-family: 'Fira Code', monospace;
            font-size: 12px;
            flex-shrink: 0;
        }

        .terminal-title {
            display: flex;
            align-items: center;
            gap: 8px;
            font-weight: 600;
        }

        .tag-food { color: var(--neon-cyan); }
        .tag-player { color: var(--neon-green); }
        .tag-network { color: var(--neon-blue); }
        .tag-physics { color: var(--neon-purple); }

        .terminal-count {
            background: #1e293b;
            color: var(--text-muted);
            padding: 1px 6px;
            border-radius: 4px;
            font-size: 10px;
        }

        .terminal-controls {
            display: flex;
            align-items: center;
            gap: 6px;
        }

        .btn-mini {
            background: #1e293b;
            border: 1px solid #334155;
            color: #94a3b8;
            font-size: 10px;
            padding: 2px 7px;
            border-radius: 4px;
            cursor: pointer;
            font-family: 'Inter', sans-serif;
            transition: 0.2s;
        }

        .btn-mini:hover { color: #fff; background: #334155; }

        .btn-mini.active {
            background: rgba(56, 189, 248, 0.2);
            color: var(--neon-blue);
            border-color: var(--neon-blue);
        }

        /* Terminal Body */
        .terminal-body {
            flex: 1;
            padding: 10px 12px;
            overflow-y: auto;
            font-family: 'Fira Code', monospace;
            font-size: 11.5px;
            line-height: 1.55;
            color: #cbd5e1;
            scroll-behavior: smooth;
        }

        .terminal-body::-webkit-scrollbar { width: 6px; }
        .terminal-body::-webkit-scrollbar-track { background: transparent; }
        .terminal-body::-webkit-scrollbar-thumb { background: #1e293b; border-radius: 3px; }

        .log-line {
            display: flex;
            align-items: flex-start;
            gap: 8px;
            padding: 2px 0;
            border-bottom: 1px solid rgba(30, 41, 59, 0.25);
            word-break: break-word;
        }

        .log-ts { color: var(--text-dim); flex-shrink: 0; font-size: 10.5px; }

        .log-badge {
            font-size: 9.5px;
            padding: 1px 5px;
            border-radius: 3px;
            font-weight: 700;
            flex-shrink: 0;
            text-transform: uppercase;
        }

        .badge-info { background: rgba(56, 189, 248, 0.15); color: var(--neon-blue); }
        .badge-success { background: rgba(34, 197, 94, 0.15); color: var(--neon-green); }
        .badge-warn { background: rgba(245, 158, 11, 0.15); color: var(--neon-amber); }
        .badge-error { background: rgba(244, 63, 94, 0.15); color: var(--neon-rose); }
        .badge-eat { background: rgba(6, 182, 212, 0.2); color: var(--neon-cyan); border: 1px solid rgba(6, 182, 212, 0.4); }
        .badge-spawn { background: rgba(168, 85, 247, 0.2); color: var(--neon-purple); border: 1px solid rgba(168, 85, 247, 0.4); }

        .log-msg { flex: 1; }

        .empty-placeholder {
            color: var(--text-dim);
            font-style: italic;
            text-align: center;
            padding: 30px 0;
        }

        /* Footer */
        footer {
            background: #090e1a;
            border-top: 1px solid var(--border-dim);
            padding: 6px 16px;
            display: flex;
            align-items: center;
            justify-content: space-between;
            font-size: 11px;
            color: var(--text-muted);
            font-family: 'Fira Code', monospace;
            flex-shrink: 0;
        }
    </style>
</head>
<body>

    <!-- Header & Auth Bar -->
    <header>
        <div class="brand">
            <div class="pulse-dot" id="status-dot"></div>
            <h1>SNAKE TELEMETRY</h1>
        </div>

        <!-- Connection & Token Box -->
        <div class="auth-bar">
            <span style="font-size: 11px; color: var(--text-muted);">WS:</span>
            <input type="text" class="auth-input input-url" id="ws-url" placeholder="ws://localhost:8080/ws/monitor">
            <span style="font-size: 11px; color: var(--text-muted);">Token:</span>
            <input type="password" class="auth-input input-token" id="ws-token" value="12345678" placeholder="Token">
            <button class="btn btn-connect" id="btn-toggle-connect" onclick="handleConnectToggle()">🔌 Connect</button>
        </div>

        <div class="stats-bar">
            <div class="stat-badge">
                <span class="label">TPS:</span>
                <span class="val val-tps" id="stat-tps">-</span>
            </div>
            <div class="stat-badge" title="Active Game Clients connected to /ws">
                <span class="label">🎮 GAME DEVICES:</span>
                <span class="val val-players" id="stat-players">0</span>
            </div>
            <div class="stat-badge" title="Admin Dashboards connected to /ws/monitor">
                <span class="label">🛡️ ADMIN SESSIONS:</span>
                <span class="val" style="color: var(--neon-purple);" id="stat-admins">0</span>
            </div>
            <div class="stat-badge">
                <span class="label">FOODS:</span>
                <span class="val val-foods" id="stat-foods">-</span>
            </div>
            <div class="stat-badge">
                <span class="label">EATEN:</span>
                <span class="val val-eaten" id="stat-eaten">-</span>
            </div>
            <div class="stat-badge">
                <span class="label">RAM:</span>
                <span class="val val-ram" id="stat-ram">-</span>
            </div>
        </div>

        <div style="display: flex; gap: 6px;">
            <button class="btn" style="background:#0284c7; border-color:#38bdf8; color:#fff;" onclick="spawnFoodBurst()">+50 Foods</button>
            <button class="btn" onclick="clearAllTerminals()">Clear</button>
        </div>
    </header>

    <!-- 4 Dedicated Terminal Panels -->
    <div class="grid-container">

        <!-- 1. FOOD TERMINAL -->
        <div class="terminal-panel panel-food">
            <div class="terminal-header">
                <div class="terminal-title tag-food">
                    [TERMINAL 1: FOOD EVENTS & CALLS]
                    <span class="terminal-count" id="count-food">0 events</span>
                </div>
                <div class="terminal-controls">
                    <button class="btn-mini active" id="autoscroll-food" onclick="toggleAutoScroll('food')">Auto-scroll: ON</button>
                    <button class="btn-mini" onclick="clearTerminal('food')">Clear</button>
                </div>
            </div>
            <div class="terminal-body" id="body-food">
                <div class="empty-placeholder">🔴 Disconnected. Enter token '12345678' and click 'Connect' to stream food events.</div>
            </div>
        </div>

        <!-- 2. PLAYER TERMINAL -->
        <div class="terminal-panel panel-player">
            <div class="terminal-header">
                <div class="terminal-title tag-player">
                    [TERMINAL 2: PLAYERS & MATCH]
                    <span class="terminal-count" id="count-player">0 events</span>
                </div>
                <div class="terminal-controls">
                    <button class="btn-mini active" id="autoscroll-player" onclick="toggleAutoScroll('player')">Auto-scroll: ON</button>
                    <button class="btn-mini" onclick="clearTerminal('player')">Clear</button>
                </div>
            </div>
            <div class="terminal-body" id="body-player">
                <div class="empty-placeholder">🔴 Disconnected. Enter token '12345678' and click 'Connect' to stream player events.</div>
            </div>
        </div>

        <!-- 3. NETWORK TERMINAL -->
        <div class="terminal-panel panel-network">
            <div class="terminal-header">
                <div class="terminal-title tag-network">
                    [TERMINAL 3: WEBSOCKET & NETWORK I/O]
                    <span class="terminal-count" id="count-network">0 events</span>
                </div>
                <div class="terminal-controls">
                    <button class="btn-mini active" id="autoscroll-network" onclick="toggleAutoScroll('network')">Auto-scroll: ON</button>
                    <button class="btn-mini" onclick="clearTerminal('network')">Clear</button>
                </div>
            </div>
            <div class="terminal-body" id="body-network">
                <div class="empty-placeholder">🔴 Disconnected. Enter token '12345678' and click 'Connect' to stream network packets.</div>
            </div>
        </div>

        <!-- 4. PHYSICS & ENGINE TERMINAL -->
        <div class="terminal-panel panel-physics">
            <div class="terminal-header">
                <div class="terminal-title tag-physics">
                    [TERMINAL 4: PHYSICS & ENGINE TICK]
                    <span class="terminal-count" id="count-physics">0 events</span>
                </div>
                <div class="terminal-controls">
                    <button class="btn-mini active" id="autoscroll-physics" onclick="toggleAutoScroll('physics')">Auto-scroll: ON</button>
                    <button class="btn-mini" onclick="clearTerminal('physics')">Clear</button>
                </div>
            </div>
            <div class="terminal-body" id="body-physics">
                <div class="empty-placeholder">🔴 Disconnected. Enter token '12345678' and click 'Connect' to stream engine ticks.</div>
            </div>
        </div>

    </div>

    <!-- Footer -->
    <footer>
        <div id="footer-status">🔴 Status: DISCONNECTED (Token Auth Required)</div>
        <div>Auth Token: <code>12345678</code> | Endpoint: <code>/ws/monitor</code></div>
    </footer>

    <script>
        var socket = null;
        var isConnected = false;

        var autoScroll = {
            food: true,
            player: true,
            network: true,
            physics: true
        };

        var counts = {
            food: 0,
            player: 0,
            network: 0,
            physics: 0
        };

        // Pre-fill default WebSocket URL based on current host
        window.addEventListener('DOMContentLoaded', function() {
            var proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
            var host = window.location.host || 'localhost:8080';
            document.getElementById('ws-url').value = proto + '//' + host + '/ws/monitor';
        });

        function handleConnectToggle() {
            if (isConnected) {
                disconnectWebSocket();
            } else {
                connectWebSocket();
            }
        }

        function connectWebSocket() {
            var urlInput = document.getElementById('ws-url').value.trim();
            var tokenInput = document.getElementById('ws-token').value.trim();

            if (!urlInput) {
                alert('Please enter a WebSocket URL');
                return;
            }

            if (!tokenInput) {
                alert('Please enter the security token (e.g. 12345678)');
                return;
            }

            var wsFullUrl = urlInput;
            if (wsFullUrl.indexOf('?') === -1) {
                wsFullUrl += '?token=' + encodeURIComponent(tokenInput);
            } else {
                wsFullUrl += '&token=' + encodeURIComponent(tokenInput);
            }

            updateStatus('CONNECTING...', false);

            try {
                socket = new WebSocket(wsFullUrl);
            } catch (e) {
                updateStatus('FAILED TO CONNECT: ' + e.message, false);
                return;
            }

            socket.onopen = function() {
                isConnected = true;
                updateStatus('🟢 CONNECTED & AUTHENTICATED (Token Verified)', true);
                clearAllTerminals();
                appendLog('network', 'success', 'Authenticated successfully with token 12345678');
            };

            socket.onclose = function(event) {
                isConnected = false;
                if (event.code === 1006 || event.code === 4001) {
                    updateStatus('🔴 REJECTED: Invalid Token or Server Offline (401 Unauthorized)', false);
                } else {
                    updateStatus('🔴 DISCONNECTED', false);
                }
                socket = null;
            };

            socket.onerror = function(err) {
                isConnected = false;
                updateStatus('🔴 Connection Error / Auth Denied', false);
            };

            socket.onmessage = function(event) {
                try {
                    var data = JSON.parse(event.data);

                    if (data.channel === 'metrics' && data.data) {
                        var m = data.data;
                        document.getElementById('stat-tps').textContent = m.tick_rate || 30;
                        document.getElementById('stat-players').textContent = m.active_players || 0;
                        if (document.getElementById('stat-admins')) {
                            document.getElementById('stat-admins').textContent = m.admin_monitors || 0;
                        }
                        document.getElementById('stat-foods').textContent = m.total_foods || 0;
                        document.getElementById('stat-eaten').textContent = m.total_eaten || 0;
                        document.getElementById('stat-ram').textContent = (m.alloc_mem_mb || 0).toFixed(1) + ' MB';
                        return;
                    }

                    appendLog(data.channel, data.level, data.msg, data.time);
                } catch (e) {
                    console.error('Failed to parse frame:', e);
                }
            };
        }

        function disconnectWebSocket() {
            if (socket) {
                socket.close();
                socket = null;
            }
            isConnected = false;
            updateStatus('🔴 DISCONNECTED', false);
        }

        function updateStatus(text, connected) {
            var dot = document.getElementById('status-dot');
            var btn = document.getElementById('btn-toggle-connect');
            var footer = document.getElementById('footer-status');

            if (connected) {
                dot.className = 'pulse-dot connected';
                btn.textContent = '❌ Disconnect';
                btn.className = 'btn btn-disconnect';
                footer.textContent = text;
            } else {
                dot.className = 'pulse-dot';
                btn.textContent = '🔌 Connect';
                btn.className = 'btn btn-connect';
                footer.textContent = 'Status: ' + text;
            }
        }

        function toggleAutoScroll(chan) {
            autoScroll[chan] = !autoScroll[chan];
            var btn = document.getElementById('autoscroll-' + chan);
            btn.textContent = 'Auto-scroll: ' + (autoScroll[chan] ? 'ON' : 'OFF');
            btn.classList.toggle('active', autoScroll[chan]);
        }

        function clearTerminal(chan) {
            var body = document.getElementById('body-' + chan);
            body.innerHTML = '<div class="empty-placeholder">Terminal log cleared.</div>';
            counts[chan] = 0;
            document.getElementById('count-' + chan).textContent = '0 events';
        }

        function clearAllTerminals() {
            ['food', 'player', 'network', 'physics'].forEach(clearTerminal);
        }

        function appendLog(channel, level, msg, time) {
            var body = document.getElementById('body-' + channel);
            if (!body) return;

            if (counts[channel] === 0) {
                body.innerHTML = '';
            }

            counts[channel]++;
            document.getElementById('count-' + channel).textContent = counts[channel] + ' events';

            var line = document.createElement('div');
            line.className = 'log-line';

            var badgeClass = 'badge-' + (level || 'info').toLowerCase();
            var timeStr = time || new Date().toLocaleTimeString();

            line.innerHTML = '<span class="log-ts">' + timeStr + '</span>' +
                '<span class="log-badge ' + badgeClass + '">' + level + '</span>' +
                '<span class="log-msg">' + escapeHtml(msg) + '</span>';

            body.appendChild(line);

            // Keep strictly the latest 25 logs per panel to keep DOM light and smooth
            while (body.children.length > 25) {
                body.removeChild(body.firstChild);
            }

            if (autoScroll[channel]) {
                body.scrollTop = body.scrollHeight;
            }
        }

        function escapeHtml(text) {
            var div = document.createElement('div');
            div.textContent = text;
            return div.innerHTML;
        }

        function spawnFoodBurst() {
            fetch('/api/monitor/action?action=spawn_foods&count=50', { method: 'POST' })
                .then(function(r) { return r.json(); })
                .then(function(data) {
                    console.log('Spawned foods:', data);
                })
                .catch(function(err) { console.error(err); });
        }
    </script>
</body>
</html>`

// HandleDashboard serves the monitoring web interface
func HandleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(DashboardHTML))
}
