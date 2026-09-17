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

        /* Multi-Terminal Grid Layout */
        .grid-container {
            flex: 1;
            display: grid;
            grid-template-columns: 1fr 1fr;
            grid-template-rows: auto 1fr 1fr;
            gap: 12px;
            padding: 12px;
            overflow-y: auto;
        }

        @media (max-width: 960px) {
            .grid-container {
                grid-template-columns: 1fr;
                grid-template-rows: auto repeat(4, 1fr);
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

        .panel-location { grid-column: 1 / -1; border-top: 3px solid var(--neon-cyan); min-height: 120px; }
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
            <button class="btn" style="background: linear-gradient(135deg, #9333ea, #c026d3); border-color: #e879f9; color: #fff; font-weight: 700;" onclick="toggleVisualizer()">🗺️ Visualizer & Bot Eater</button>
            <button class="btn" style="background:#0284c7; border-color:#38bdf8; color:#fff;" onclick="spawnFoodBurst()">+50 Foods</button>
            <button class="btn" onclick="clearAllTerminals()">Clear</button>
        </div>
    </header>

    <!-- 5 Dedicated Terminal Panels -->
    <div class="grid-container">

        <!-- 0. REAL-TIME MULTI-DEVICE LOCATIONS & FPS TERMINAL -->
        <div class="terminal-panel panel-location">
            <div class="terminal-header">
                <div class="terminal-title" style="color: var(--neon-cyan);">
                    <span>📍 [TERMINAL 0: REAL-TIME DEVICE STATE & LOCATIONS - MULTI-DEVICE SUPPORT]</span>
                    <span class="terminal-count" id="count-devices">0 Devices Active</span>
                </div>
                <div class="terminal-controls">
                    <span style="font-size: 11px; color: var(--neon-green); font-family:'Fira Code', monospace; font-weight: 600;">⚡ Live Stream Active</span>
                </div>
            </div>
            <div class="terminal-body" id="body-devices" style="padding: 10px; display: grid; grid-template-columns: repeat(auto-fill, minmax(300px, 1fr)); gap: 10px; align-content: flex-start; max-height: 220px; overflow-y: auto;">
                <div class="empty-placeholder" id="devices-empty" style="grid-column: 1 / -1;">🔴 No active client devices streaming location. Start the Android game client to see live X, Y, Angle & Stream FPS.</div>
            </div>
        </div>

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

                    if (data.channel === 'locations') {
                        if (data.level === 'remove' && data.data && data.data.id) {
                            var card = document.getElementById('dev-card-' + data.data.id);
                            if (card) card.remove();
                            var container = document.getElementById('body-devices');
                            if (container && container.getElementsByClassName('device-card').length === 0) {
                                container.innerHTML = '<div class="empty-placeholder" id="devices-empty" style="grid-column: 1 / -1;">🔴 No active client devices streaming location. Start the Android game client to see live X, Y, Angle & Stream FPS.</div>';
                            }
                        } else if (data.data) {
                            updateSingleDeviceCard(data.data);
                        }
                        return;
                    }

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
                        
                        renderDevices(m.devices || []);
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

        function updateSingleDeviceCard(dev) {
            var container = document.getElementById('body-devices');
            if (!container || !dev || !dev.id) return;

            var emptyPlaceholder = document.getElementById('devices-empty');
            if (emptyPlaceholder) {
                emptyPlaceholder.remove();
            }

            var cardId = 'dev-card-' + dev.id;
            var card = document.getElementById(cardId);
            var fpsVal = (dev.fps || 0).toFixed(1);
            var angleRad = (dev.angle || 0).toFixed(2);
            var angleDeg = (dev.angle_deg || 0).toFixed(1);

            if (!card) {
                card = document.createElement('div');
                card.id = cardId;
                card.className = 'device-card';
                card.style.cssText = 'background: rgba(15, 23, 42, 0.95); border: 1px solid rgba(56, 189, 248, 0.3); border-radius: 8px; padding: 10px; font-family: "Fira Code", monospace; box-shadow: 0 4px 12px rgba(0,0,0,0.3);';
                
                card.innerHTML = 
                    '<div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:6px;">' +
                        '<span style="font-weight:700; color:#38bdf8; font-size:12px;">🎮 <span id="dev-name-' + dev.id + '">' + (dev.name || dev.id) + '</span></span>' +
                        '<span style="background:rgba(34,197,94,0.2); color:#22c55e; border:1px solid #22c55e; padding:1px 6px; border-radius:4px; font-size:10px; font-weight:700;">🟢 <span id="dev-fps-' + dev.id + '">' + fpsVal + '</span> FPS</span>' +
                    '</div>' +
                    '<div style="display:grid; grid-template-columns:1fr 1fr; gap:4px; font-size:11px; color:#cbd5e1;">' +
                        '<div>📍 X: <span style="color:#fff; font-weight:700;" id="dev-x-' + dev.id + '">' + dev.x.toFixed(1) + '</span></div>' +
                        '<div>📍 Y: <span style="color:#fff; font-weight:700;" id="dev-y-' + dev.id + '">' + dev.y.toFixed(1) + '</span></div>' +
                        '<div>📐 Angle: <span style="color:#f59e0b; font-weight:700;" id="dev-angle-' + dev.id + '">' + angleRad + ' rad</span></div>' +
                        '<div>🔄 Heading: <span style="color:#a855f7; font-weight:700;" id="dev-deg-' + dev.id + '">' + angleDeg + '°</span></div>' +
                    '</div>' +
                    '<div style="margin-top:6px; font-size:9.5px; color:#64748b; display:flex; justify-content:space-between;">' +
                        '<span>Packets: <span id="dev-pkts-' + dev.id + '">' + (dev.packets || 0) + '</span></span>' +
                        '<span>ID: ' + dev.id + '</span>' +
                    '</div>';
                container.appendChild(card);
            } else {
                // High-speed text node mutation (super fast, 60+ FPS instantaneous response)
                var xEl = document.getElementById('dev-x-' + dev.id);
                var yEl = document.getElementById('dev-y-' + dev.id);
                var angleEl = document.getElementById('dev-angle-' + dev.id);
                var degEl = document.getElementById('dev-deg-' + dev.id);
                var fpsEl = document.getElementById('dev-fps-' + dev.id);
                var pktsEl = document.getElementById('dev-pkts-' + dev.id);
                var nameEl = document.getElementById('dev-name-' + dev.id);

                if (xEl) xEl.textContent = dev.x.toFixed(1);
                if (yEl) yEl.textContent = dev.y.toFixed(1);
                if (angleEl) angleEl.textContent = angleRad + ' rad';
                if (degEl) degEl.textContent = angleDeg + '°';
                if (fpsEl) fpsEl.textContent = fpsVal;
                if (pktsEl) pktsEl.textContent = dev.packets || 0;
                if (nameEl && dev.name) nameEl.textContent = dev.name;
            }

            var countEl = document.getElementById('count-devices');
            if (countEl) {
                var total = container.getElementsByClassName('device-card').length;
                countEl.textContent = total + ' Device(s) Active';
            }
        }

        function renderDevices(devices) {
            var countEl = document.getElementById('count-devices');
            var container = document.getElementById('body-devices');
            if (!container) return;

            if (countEl) {
                countEl.textContent = (devices ? devices.length : 0) + ' Device(s) Active';
            }

            if (!devices || devices.length === 0) {
                container.innerHTML = '<div class="empty-placeholder" id="devices-empty" style="grid-column: 1 / -1;">🔴 No active client devices streaming location. Start the Android game client to see live X, Y, Angle & Stream FPS.</div>';
                return;
            }

            var activeIds = {};
            devices.forEach(function(dev) {
                activeIds[dev.id] = true;
                updateSingleDeviceCard(dev);
            });

            // Clean up disconnected devices
            var existingCards = container.getElementsByClassName('device-card');
            for (var i = existingCards.length - 1; i >= 0; i--) {
                var c = existingCards[i];
                var cId = c.id.replace('dev-card-', '');
                if (!activeIds[cId]) {
                    c.remove();
                }
            }
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

        /* ==========================================================================
           🗺️ INTERACTIVE ARENA VISUALIZER & BOT EATER CONTROLLER
           ========================================================================== */
        var vizActive = false;
        var vizCanvas, vizCtx;
        var vizScale = 1.0;
        var vizRadius = 1500;
        var vizMouseWorld = { x: 15000, y: 15000, isOver: false, isDown: false };
        var vizPlayers = [];
        var vizRipples = [];
        var vizBotTotalEaten = 0;
        var vizBotTotalScore = 0;
        var vizPollTimer = null;
        var vizAutoPatrol = false;
        var vizPatrolAngle = 0;
        var vizPatrolPos = { x: 15000, y: 15000 };

        function toggleVisualizer() {
            var modal = document.getElementById('viz-modal');
            if (!modal) return;
            vizActive = !vizActive;
            if (vizActive) {
                modal.style.display = 'flex';
                initVisualizerCanvas();
                startVisualizerLoop();
            } else {
                modal.style.display = 'none';
                stopVisualizerLoop();
            }
        }

        function initVisualizerCanvas() {
            vizCanvas = document.getElementById('viz-canvas');
            if (!vizCanvas) return;
            vizCtx = vizCanvas.getContext('2d');

            // Handle resize
            var size = Math.min(vizCanvas.parentElement.clientWidth - 20, vizCanvas.parentElement.clientHeight - 20, 680);
            if (size < 320) size = 320;
            vizCanvas.width = size;
            vizCanvas.height = size;
            vizScale = size / 30000.0;

            vizCanvas.onmousemove = function(e) {
                var rect = vizCanvas.getBoundingClientRect();
                var cx = e.clientX - rect.left;
                var cy = e.clientY - rect.top;
                vizMouseWorld.x = Math.max(0, Math.min(30000, cx / vizScale));
                vizMouseWorld.y = Math.max(0, Math.min(30000, cy / vizScale));
                vizMouseWorld.isOver = true;

                var coordEl = document.getElementById('viz-mouse-coord');
                if (coordEl) coordEl.textContent = 'X: ' + Math.round(vizMouseWorld.x) + ', Y: ' + Math.round(vizMouseWorld.y);

                if (vizMouseWorld.isDown && document.getElementById('viz-mode-drag').checked) {
                    triggerBotEat(vizMouseWorld.x, vizMouseWorld.y, vizRadius);
                }
            };

            vizCanvas.onmouseleave = function() {
                vizMouseWorld.isOver = false;
                vizMouseWorld.isDown = false;
            };

            vizCanvas.onmousedown = function(e) {
                if (e.button !== 0) return;
                vizMouseWorld.isDown = true;
                triggerBotEat(vizMouseWorld.x, vizMouseWorld.y, vizRadius);
            };

            vizCanvas.onmouseup = function() {
                vizMouseWorld.isDown = false;
            };

            // Radius Slider
            var slider = document.getElementById('viz-radius-slider');
            if (slider) {
                slider.oninput = function() {
                    vizRadius = parseFloat(this.value);
                    document.getElementById('viz-radius-val').textContent = vizRadius + ' units';
                };
            }
        }

        function triggerBotEat(wx, wy, radius) {
            var botName = document.getElementById('viz-bot-name') ? document.getElementById('viz-bot-name').value : 'bot_tester';
            if (!botName) botName = 'bot_tester';

            // Add visual ripple
            vizRipples.push({
                x: wx,
                y: wy,
                radius: 10,
                maxRadius: radius,
                alpha: 1.0,
                text: 'Eating...'
            });

            var url = '/api/monitor/action?action=bot_eat_area&x=' + wx.toFixed(1) + '&y=' + wy.toFixed(1) + '&radius=' + radius.toFixed(1) + '&bot_id=' + encodeURIComponent(botName);

            fetch(url, { method: 'POST' })
                .then(function(r) { return r.json(); })
                .then(function(data) {
                    if (data && data.status === 'ok') {
                        vizBotTotalEaten += (data.foods_eaten || 0);
                        vizBotTotalScore += (data.score_gained || 0);

                        var eatenEl = document.getElementById('viz-bot-eaten');
                        var scoreEl = document.getElementById('viz-bot-score');
                        var lastEl = document.getElementById('viz-last-action');

                        if (eatenEl) eatenEl.textContent = vizBotTotalEaten;
                        if (scoreEl) scoreEl.textContent = vizBotTotalScore;
                        if (lastEl) {
                            lastEl.textContent = (data.foods_eaten > 0) 
                                ? ('🍎 Consumed ' + data.foods_eaten + ' food(s) (+' + data.score_gained + ' pts)') 
                                : 'No foods in this circle';
                            lastEl.style.color = (data.foods_eaten > 0) ? '#22c55e' : '#94a3b8';
                        }
                    }
                })
                .catch(function(err) {
                    console.error('Bot eat request error:', err);
                });
        }

        function startVisualizerLoop() {
            fetchLivePlayers();
            if (vizPollTimer) clearInterval(vizPollTimer);
            vizPollTimer = setInterval(fetchLivePlayers, 300);
            requestAnimationFrame(renderVisualizerFrame);
        }

        function stopVisualizerLoop() {
            if (vizPollTimer) {
                clearInterval(vizPollTimer);
                vizPollTimer = null;
            }
        }

        function fetchLivePlayers() {
            if (!vizActive) return;
            fetch('/api/players/live')
                .then(function(r) { return r.json(); })
                .then(function(data) {
                    if (data && data.players) {
                        vizPlayers = data.players;
                        updateVizPlayerList(vizPlayers);
                    }
                })
                .catch(function(err) { /* silent */ });
        }

        function updateVizPlayerList(players) {
            var listEl = document.getElementById('viz-player-list');
            if (!listEl) return;
            if (!players || players.length === 0) {
                listEl.innerHTML = '<div style="color: #64748b; font-size: 11px; padding: 6px;">No live players in arena</div>';
                return;
            }
            var html = '';
            players.forEach(function(p) {
                html += '<div style="display:flex; justify-content:space-between; align-items:center; background:rgba(15,23,42,0.8); border:1px solid #1e293b; border-radius:5px; padding:5px 8px; margin-bottom:4px; font-size:11px;">' +
                    '<div><span style="color:#22c55e; font-weight:700;">🟢 ' + escapeHtml(p.name || p.id) + '</span></div>' +
                    '<div style="color:#94a3b8; font-family:\'Fira Code\',monospace;">(' + Math.round(p.x) + ', ' + Math.round(p.y) + ') | <span style="color:#f59e0b; font-weight:700;">' + p.score + '</span></div>' +
                '</div>';
            });
            listEl.innerHTML = html;
        }

        function renderVisualizerFrame() {
            if (!vizActive || !vizCanvas || !vizCtx) return;

            var w = vizCanvas.width;
            var h = vizCanvas.height;

            // 1. Clear background
            vizCtx.fillStyle = '#050811';
            vizCtx.fillRect(0, 0, w, h);

            // 2. Draw Spatial Grid (every 3000 world units)
            vizCtx.strokeStyle = 'rgba(30, 41, 59, 0.45)';
            vizCtx.lineWidth = 1;
            for (var gx = 0; gx <= 30000; gx += 3000) {
                var cx = gx * vizScale;
                vizCtx.beginPath();
                vizCtx.moveTo(cx, 0);
                vizCtx.lineTo(cx, h);
                vizCtx.stroke();
            }
            for (var gy = 0; gy <= 30000; gy += 3000) {
                var cy = gy * vizScale;
                vizCtx.beginPath();
                vizCtx.moveTo(0, cy);
                vizCtx.lineTo(w, cy);
                vizCtx.stroke();
            }

            // 3. Draw Playable Arena Border (220 to 29780)
            var bMargin = 220 * vizScale;
            var bW = (30000 - 440) * vizScale;
            vizCtx.strokeStyle = 'rgba(244, 63, 94, 0.75)';
            vizCtx.lineWidth = 2;
            vizCtx.setLineDash([6, 4]);
            vizCtx.strokeRect(bMargin, bMargin, bW, bW);
            vizCtx.setLineDash([]);

            // Label Border
            vizCtx.fillStyle = '#f43f5e';
            vizCtx.font = '9px Fira Code, monospace';
            vizCtx.fillText('BORDER (220, 220)', bMargin + 4, bMargin + 12);
            vizCtx.fillText('30,000 x 30,000 ARENA', w - 140, h - 8);

            // 4. Draw Active Players / Snakes
            vizPlayers.forEach(function(p) {
                var px = p.x * vizScale;
                var py = p.y * vizScale;

                // Player AoI Preview Box (optional subtle ring)
                vizCtx.strokeStyle = 'rgba(56, 189, 248, 0.2)';
                vizCtx.lineWidth = 1;
                vizCtx.beginPath();
                vizCtx.arc(px, py, 2400 * vizScale, 0, Math.PI * 2);
                vizCtx.stroke();

                // Player Head Dot (Neon Green Glow)
                vizCtx.shadowColor = '#22c55e';
                vizCtx.shadowBlur = 10;
                vizCtx.fillStyle = '#22c55e';
                vizCtx.beginPath();
                vizCtx.arc(px, py, 5, 0, Math.PI * 2);
                vizCtx.fill();

                // Direction Vector Arrow
                var angle = p.angle || 0;
                var arrowLen = 14;
                vizCtx.strokeStyle = '#fff';
                vizCtx.lineWidth = 2;
                vizCtx.beginPath();
                vizCtx.moveTo(px, py);
                vizCtx.lineTo(px + Math.cos(angle) * arrowLen, py + Math.sin(angle) * arrowLen);
                vizCtx.stroke();
                vizCtx.shadowBlur = 0;

                // Player Name Tag & Score
                vizCtx.fillStyle = '#fff';
                vizCtx.font = 'bold 10px Inter, sans-serif';
                vizCtx.fillText(p.name || p.id, px + 8, py - 4);
                vizCtx.fillStyle = '#f59e0b';
                vizCtx.font = '9px Fira Code, monospace';
                vizCtx.fillText('Score: ' + p.score, px + 8, py + 8);
            });

            // 5. Draw Auto-Patrol Bot if active
            if (vizAutoPatrol) {
                vizPatrolAngle += 0.03;
                vizPatrolPos.x = 15000 + Math.cos(vizPatrolAngle) * 8000;
                vizPatrolPos.y = 15000 + Math.sin(vizPatrolAngle * 1.3) * 8000;
                triggerBotEat(vizPatrolPos.x, vizPatrolPos.y, vizRadius);

                var bx = vizPatrolPos.x * vizScale;
                var by = vizPatrolPos.y * vizScale;
                vizCtx.strokeStyle = '#a855f7';
                vizCtx.lineWidth = 2;
                vizCtx.beginPath();
                vizCtx.arc(bx, by, vizRadius * vizScale, 0, Math.PI * 2);
                vizCtx.stroke();
            }

            // 6. Draw Shockwave Ripples
            for (var i = vizRipples.length - 1; i >= 0; i--) {
                var rip = vizRipples[i];
                rip.radius += (rip.maxRadius - rip.radius) * 0.18 + 2;
                rip.alpha -= 0.035;

                if (rip.alpha <= 0) {
                    vizRipples.splice(i, 1);
                    continue;
                }

                var rx = rip.x * vizScale;
                var ry = rip.y * vizScale;
                var rRad = rip.radius * vizScale;

                vizCtx.strokeStyle = 'rgba(232, 121, 249, ' + rip.alpha + ')';
                vizCtx.lineWidth = 2.5;
                vizCtx.beginPath();
                vizCtx.arc(rx, ry, rRad, 0, Math.PI * 2);
                vizCtx.stroke();

                vizCtx.fillStyle = 'rgba(244, 114, 182, ' + (rip.alpha * 0.15) + ')';
                vizCtx.fill();
            }

            // 7. Draw Interactive Bot Eater Circle at Mouse
            if (vizMouseWorld.isOver) {
                var mx = vizMouseWorld.x * vizScale;
                var my = vizMouseWorld.y * vizScale;
                var mRad = vizRadius * vizScale;

                vizCtx.shadowColor = '#d946ef';
                vizCtx.shadowBlur = 12;
                vizCtx.strokeStyle = '#e879f9';
                vizCtx.lineWidth = 2;
                vizCtx.beginPath();
                vizCtx.arc(mx, my, mRad, 0, Math.PI * 2);
                vizCtx.stroke();

                vizCtx.fillStyle = 'rgba(217, 70, 239, 0.12)';
                vizCtx.fill();
                vizCtx.shadowBlur = 0;

                // Center crosshair
                vizCtx.strokeStyle = '#f472b6';
                vizCtx.lineWidth = 1;
                vizCtx.beginPath();
                vizCtx.moveTo(mx - 6, my);
                vizCtx.lineTo(mx + 6, my);
                vizCtx.moveTo(mx, my - 6);
                vizCtx.lineTo(mx, my + 6);
                vizCtx.stroke();

                // Circle Info Tag
                vizCtx.fillStyle = '#e879f9';
                vizCtx.font = 'bold 9.5px Fira Code, monospace';
                vizCtx.fillText('BOT EATER (R: ' + vizRadius + ')', mx + mRad + 6, my);
            }

            requestAnimationFrame(renderVisualizerFrame);
        }

        function toggleAutoPatrol() {
            vizAutoPatrol = !vizAutoPatrol;
            var btn = document.getElementById('btn-auto-patrol');
            if (btn) {
                btn.textContent = vizAutoPatrol ? '🛑 Stop Auto-Patrol' : '🚀 Start Auto-Patrol';
                btn.style.background = vizAutoPatrol ? '#e11d48' : '#7c3aed';
            }
        }

        function resetBotStats() {
            vizBotTotalEaten = 0;
            vizBotTotalScore = 0;
            var eatenEl = document.getElementById('viz-bot-eaten');
            var scoreEl = document.getElementById('viz-bot-score');
            var lastEl = document.getElementById('viz-last-action');
            if (eatenEl) eatenEl.textContent = '0';
            if (scoreEl) scoreEl.textContent = '0';
            if (lastEl) lastEl.textContent = 'Reset';
        }

        function eatCenter() {
            triggerBotEat(15000, 15000, vizRadius);
        }

        function eatNearPlayer() {
            if (vizPlayers && vizPlayers.length > 0) {
                var p = vizPlayers[0];
                triggerBotEat(p.x, p.y, vizRadius);
            } else {
                alert('No active player in arena to target.');
            }
        }
    </script>

    <!-- 🗺️ ARENA VISUALIZER & INTERACTIVE BOT EATER MODAL -->
    <div id="viz-modal" style="display:none; position:fixed; top:0; left:0; width:100vw; height:100vh; background:rgba(5, 8, 17, 0.92); backdrop-filter:blur(12px); z-index:99999; flex-direction:column; padding:16px; box-sizing:border-box;">
        <!-- Modal Top Bar -->
        <div style="display:flex; justify-content:space-between; align-items:center; border-bottom:1px solid var(--border-dim); padding-bottom:10px; margin-bottom:12px;">
            <div style="display:flex; align-items:center; gap:12px;">
                <span style="font-size:18px;">🗺️</span>
                <div>
                    <h2 style="font-size:15px; font-weight:700; color:#fff; letter-spacing:-0.3px;">Arena Visualizer & Interactive Bot Eater (30k × 30k Canvas)</h2>
                    <p style="font-size:11px; color:#94a3b8;">Click or drag the circle to simulate an authoritative Eater Bot consuming all foods in that radius.</p>
                </div>
            </div>
            <div style="display:flex; align-items:center; gap:8px;">
                <button class="btn" style="background:#0284c7; border-color:#38bdf8; color:#fff;" onclick="spawnFoodBurst()">+50 Foods</button>
                <button class="btn btn-disconnect" onclick="toggleVisualizer()">✕ Close Visualizer</button>
            </div>
        </div>

        <!-- Modal Body: Canvas on Left, Controls on Right -->
        <div style="display:flex; gap:16px; flex:1; min-height:0; overflow:hidden;">
            <!-- Canvas Container -->
            <div style="flex:1; display:flex; justify-content:center; align-items:center; background:#070b16; border:1px solid var(--border-dim); border-radius:8px; padding:10px; position:relative; overflow:hidden;">
                <canvas id="viz-canvas" style="background:#050811; border:1px solid #1e293b; border-radius:6px; cursor:crosshair; box-shadow:0 0 25px rgba(0,0,0,0.8);"></canvas>
                <!-- Live Mouse Coordinate Overlay -->
                <div style="position:absolute; bottom:16px; left:16px; background:rgba(15,23,42,0.85); border:1px solid #334155; padding:4px 10px; border-radius:5px; font-family:'Fira Code',monospace; font-size:11px; color:#38bdf8;">
                    📍 Canvas World: <span id="viz-mouse-coord" style="font-weight:700; color:#fff;">X: 15000, Y: 15000</span>
                </div>
            </div>

            <!-- Controls & Live Stats Sidebar -->
            <div style="width:340px; display:flex; flex-direction:column; gap:12px; overflow-y:auto;">
                <!-- Card 1: Bot Settings -->
                <div style="background:#0d1322; border:1px solid var(--border-dim); border-radius:8px; padding:12px;">
                    <div style="font-size:12px; font-weight:700; color:#e879f9; margin-bottom:10px; display:flex; justify-content:space-between; align-items:center;">
                        <span>🤖 BOT EATER CONFIGURATION</span>
                        <span style="font-size:10px; color:#94a3b8;">Test Tool</span>
                    </div>

                    <!-- Radius Slider -->
                    <div style="margin-bottom:12px;">
                        <div style="display:flex; justify-content:space-between; font-size:11px; margin-bottom:4px;">
                            <span style="color:#94a3b8;">Circle Radius:</span>
                            <span style="color:#e879f9; font-weight:700; font-family:'Fira Code',monospace;" id="viz-radius-val">1500 units</span>
                        </div>
                        <input type="range" id="viz-radius-slider" min="300" max="5000" step="100" value="1500" style="width:100%; cursor:pointer;">
                        <div style="display:flex; justify-content:space-between; font-size:9px; color:#64748b; margin-top:2px;">
                            <span>300 (1 Grid)</span>
                            <span>2400 (AoI Viewport)</span>
                            <span>5000 (Mega)</span>
                        </div>
                    </div>

                    <!-- Bot Name Input -->
                    <div style="margin-bottom:12px;">
                        <label style="font-size:11px; color:#94a3b8; display:block; margin-bottom:4px;">Bot ID / Name:</label>
                        <input type="text" id="viz-bot-name" class="auth-input" value="bot_tester" style="width:100%;">
                    </div>

                    <!-- Mode Select -->
                    <div style="margin-bottom:10px; font-size:11px;">
                        <span style="color:#94a3b8; display:block; margin-bottom:6px;">Interaction Mode:</span>
                        <div style="display:flex; gap:12px;">
                            <label style="display:flex; align-items:center; gap:4px; color:#fff; cursor:pointer;">
                                <input type="radio" name="viz-mode" id="viz-mode-click" checked> Single Click
                            </label>
                            <label style="display:flex; align-items:center; gap:4px; color:#fff; cursor:pointer;">
                                <input type="radio" name="viz-mode" id="viz-mode-drag"> Drag Stream
                            </label>
                        </div>
                    </div>

                    <!-- Auto Patrol Button -->
                    <button class="btn" id="btn-auto-patrol" style="width:100%; justify-content:center; background:#7c3aed; border-color:#a855f7; color:#fff;" onclick="toggleAutoPatrol()">🚀 Start Auto-Patrol Bot</button>
                </div>

                <!-- Card 2: Live Stats -->
                <div style="background:#0d1322; border:1px solid var(--border-dim); border-radius:8px; padding:12px;">
                    <div style="font-size:12px; font-weight:700; color:#38bdf8; margin-bottom:10px;">📊 BOT EATING METRICS</div>
                    <div style="display:grid; grid-template-columns:1fr 1fr; gap:8px; margin-bottom:10px;">
                        <div style="background:#090e1a; border:1px solid #1e293b; padding:8px; border-radius:6px; text-align:center;">
                            <div style="font-size:10px; color:#94a3b8;">FOODS EATEN</div>
                            <div style="font-size:18px; font-weight:700; color:#22c55e; font-family:'Fira Code',monospace;" id="viz-bot-eaten">0</div>
                        </div>
                        <div style="background:#090e1a; border:1px solid #1e293b; padding:8px; border-radius:6px; text-align:center;">
                            <div style="font-size:10px; color:#94a3b8;">SCORE GAINED</div>
                            <div style="font-size:18px; font-weight:700; color:#f59e0b; font-family:'Fira Code',monospace;" id="viz-bot-score">0</div>
                        </div>
                    </div>
                    <div style="font-size:11px; color:#94a3b8; background:#090e1a; padding:6px 8px; border-radius:5px; border:1px solid #1e293b;">
                        Last: <span id="viz-last-action" style="color:#fff; font-weight:600;">Ready to Eat</span>
                    </div>
                </div>

                <!-- Card 3: Connected Live Players -->
                <div style="background:#0d1322; border:1px solid var(--border-dim); border-radius:8px; padding:12px; flex:1; display:flex; flex-direction:column; min-height:120px;">
                    <div style="font-size:12px; font-weight:700; color:#22c55e; margin-bottom:8px;">🎮 LIVE CLIENTS IN ARENA</div>
                    <div id="viz-player-list" style="flex:1; overflow-y:auto; max-height:140px;">
                        <div style="color:#64748b; font-size:11px;">Loading players...</div>
                    </div>
                </div>

                <!-- Quick Action Buttons -->
                <div style="display:flex; flex-direction:column; gap:6px;">
                    <div style="display:flex; gap:6px;">
                        <button class="btn" style="flex:1; justify-content:center;" onclick="eatNearPlayer()">🎯 Eat Around Player</button>
                        <button class="btn" style="flex:1; justify-content:center;" onclick="eatCenter()">🎯 Eat Center</button>
                    </div>
                    <button class="btn" style="justify-content:center;" onclick="resetBotStats()">🔄 Reset Bot Stats</button>
                </div>
            </div>
        </div>
    </div>
</body>
</html>`

// HandleDashboard serves the monitoring web interface
func HandleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(DashboardHTML))
}
