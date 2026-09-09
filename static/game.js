// Neon Snake - Real-time WebSocket Client
const canvas = document.getElementById('gameCanvas');
const ctx = canvas.getContext('2d');

const statusDot = document.getElementById('statusDot');
const statusText = document.getElementById('statusText');
const onlineCount = document.getElementById('onlineCount');
const pingText = document.getElementById('pingText');
const joinOverlay = document.getElementById('joinOverlay');
const deathOverlay = document.getElementById('deathOverlay');
const playerNameInput = document.getElementById('playerNameInput');
const joinBtn = document.getElementById('joinBtn');
const respawnBtn = document.getElementById('respawnBtn');
const finalScore = document.getElementById('finalScore');
const leaderboardList = document.getElementById('leaderboardList');

let socket = null;
let myPlayerId = null;
let myPlayerName = 'Player1';
let isJoined = false;
let lastPingTime = 0;
let gameState = null;

const GRID_SIZE = 20; // 40x30 grid for 800x600 canvas

// Connect WebSocket
function connectWebSocket() {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/ws`;

    statusText.textContent = 'Connecting...';
    statusDot.className = 'status-dot';

    socket = new WebSocket(wsUrl);

    socket.onopen = () => {
        statusText.textContent = 'Connected (Online)';
        statusDot.className = 'status-dot online';
        pingText.textContent = 'OK';
        console.log('Connected to Snake WebSocket Server');
    };

    socket.onmessage = (event) => {
        // Can receive multiple newline-delimited JSON objects
        const lines = event.data.split('\n');
        for (const line of lines) {
            if (!line.trim()) continue;
            try {
                const msg = JSON.parse(line);
                handleServerMessage(msg);
            } catch (err) {
                console.error('JSON parse error:', err, line);
            }
        }
    };

    socket.onclose = () => {
        statusText.textContent = 'Disconnected';
        statusDot.className = 'status-dot';
        onlineCount.textContent = '0';
        console.log('WebSocket connection closed. Reconnecting in 2s...');
        setTimeout(connectWebSocket, 2000);
    };

    socket.onerror = (err) => {
        console.error('WebSocket Error:', err);
    };
}

function handleServerMessage(msg) {
    if (msg.type === 'init') {
        myPlayerId = msg.playerId;
        console.log('Assigned Player ID:', myPlayerId);
    } else if (msg.type === 'state') {
        gameState = msg.state;
        if (msg.connected !== undefined) {
            onlineCount.textContent = msg.connected;
        }
        updateLeaderboard(gameState.leaderboard);
        checkPlayerStatus();
    }
}

function checkPlayerStatus() {
    if (!gameState || !myPlayerId || !isJoined) return;
    const mySnake = gameState.snakes[myPlayerId];
    if (mySnake) {
        if (!mySnake.alive && deathOverlay.classList.contains('hidden')) {
            finalScore.textContent = mySnake.score;
            deathOverlay.classList.remove('hidden');
        } else if (mySnake.alive && !deathOverlay.classList.contains('hidden')) {
            deathOverlay.classList.add('hidden');
        }
    }
}

function updateLeaderboard(leaderboard) {
    if (!leaderboard || leaderboard.length === 0) {
        leaderboardList.innerHTML = '<div class="empty-state">No players currently online</div>';
        return;
    }

    let html = '';
    leaderboard.forEach((item, index) => {
        html += `
            <div class="leaderboard-item" style="border-left-color: ${item.color}">
                <div class="player-info">
                    <span class="player-rank">#${index + 1}</span>
                    <span class="player-name">${escapeHtml(item.name)}</span>
                </div>
                <span class="player-score">${item.score}</span>
            </div>
        `;
    });
    leaderboardList.innerHTML = html;
}

function sendDirection(dir) {
    if (socket && socket.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({
            type: 'direction',
            direction: dir
        }));
    }
}

function joinGame() {
    const name = playerNameInput.value.trim() || 'Player1';
    myPlayerName = name;
    if (socket && socket.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({
            type: 'join',
            name: name
        }));
        joinOverlay.classList.add('hidden');
        isJoined = true;
    }
}

function respawnGame() {
    if (socket && socket.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({
            type: 'respawn'
        }));
        deathOverlay.classList.add('hidden');
    }
}

// Event Listeners
joinBtn.addEventListener('click', joinGame);
playerNameInput.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') joinGame();
});

respawnBtn.addEventListener('click', respawnGame);

window.addEventListener('keydown', (e) => {
    if (!isJoined) return;

    if (e.code === 'Space') {
        if (!deathOverlay.classList.contains('hidden')) {
            respawnGame();
        }
        return;
    }

    switch (e.key) {
        case 'ArrowUp':
        case 'w':
        case 'W':
            sendDirection('UP');
            e.preventDefault();
            break;
        case 'ArrowDown':
        case 's':
        case 'S':
            sendDirection('DOWN');
            e.preventDefault();
            break;
        case 'ArrowLeft':
        case 'a':
        case 'A':
            sendDirection('LEFT');
            e.preventDefault();
            break;
        case 'ArrowRight':
        case 'd':
        case 'D':
            sendDirection('RIGHT');
            e.preventDefault();
            break;
    }
});

// Mobile D-pad
document.getElementById('btnUp').addEventListener('click', () => sendDirection('UP'));
document.getElementById('btnDown').addEventListener('click', () => sendDirection('DOWN'));
document.getElementById('btnLeft').addEventListener('click', () => sendDirection('LEFT'));
document.getElementById('btnRight').addEventListener('click', () => sendDirection('RIGHT'));

// Canvas Render Loop
function render() {
    requestAnimationFrame(render);

    // Clear background with faint neon grid
    ctx.fillStyle = '#0a0b12';
    ctx.fillRect(0, 0, canvas.width, canvas.height);

    // Draw Grid
    ctx.strokeStyle = 'rgba(255, 255, 255, 0.03)';
    ctx.lineWidth = 1;
    for (let x = 0; x < canvas.width; x += GRID_SIZE) {
        ctx.beginPath();
        ctx.moveTo(x, 0);
        ctx.lineTo(x, canvas.height);
        ctx.stroke();
    }
    for (let y = 0; y < canvas.height; y += GRID_SIZE) {
        ctx.beginPath();
        ctx.moveTo(0, y);
        ctx.lineTo(canvas.width, y);
        ctx.stroke();
    }

    if (!gameState) return;

    // Scale calculation
    const scaleX = canvas.width / gameState.width;
    const scaleY = canvas.height / gameState.height;

    // Draw Foods
    gameState.foods.forEach(food => {
        const fx = food.x * scaleX + scaleX / 2;
        const fy = food.y * scaleY + scaleY / 2;
        const radius = scaleX / 2 - 2;

        ctx.save();
        ctx.shadowBlur = 15;
        ctx.shadowColor = food.color || '#ff007f';
        ctx.fillStyle = food.color || '#ff007f';

        ctx.beginPath();
        ctx.arc(fx, fy, radius, 0, Math.PI * 2);
        ctx.fill();
        ctx.restore();
    });

    // Draw Snakes
    for (const id in gameState.snakes) {
        const snake = gameState.snakes[id];
        if (!snake.alive) continue;

        const isMe = (id === myPlayerId);
        const color = snake.color || '#00ffcc';

        ctx.save();
        ctx.shadowBlur = isMe ? 20 : 10;
        ctx.shadowColor = color;
        ctx.fillStyle = color;

        // Draw body
        snake.body.forEach((pt, index) => {
            const px = pt.x * scaleX;
            const py = pt.y * scaleY;
            const size = scaleX - 2;

            if (index === 0) {
                // Head (slightly larger / rounded)
                ctx.fillStyle = isMe ? '#ffffff' : color;
                ctx.fillRect(px + 1, py + 1, size, size);
            } else {
                ctx.fillStyle = color;
                ctx.fillRect(px + 1, py + 1, size, size);
            }
        });

        // Draw Player Name Tag above head
        if (snake.body.length > 0) {
            const head = snake.body[0];
            ctx.shadowBlur = 0;
            ctx.fillStyle = isMe ? '#00ffcc' : '#ffffff';
            ctx.font = 'bold 11px Outfit, sans-serif';
            ctx.textAlign = 'center';
            ctx.fillText(snake.name, head.x * scaleX + scaleX / 2, head.y * scaleY - 6);
        }

        ctx.restore();
    }
}

function escapeHtml(text) {
    const div = document.createElement('div');
    div.innerText = text;
    return div.innerHTML;
}

// Start
connectWebSocket();
render();
