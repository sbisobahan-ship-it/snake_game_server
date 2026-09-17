# 🚀 Snake Slither Pro - Server Integration & Protocol Guide

This document contains the complete server endpoints, connection URLs, binary network protocol specifications, and a ready-to-use AI/Developer Prompt for integrating the Android (Kotlin) client.

---

## 🌐 1. Server Endpoints & URLs

| Service / Endpoint | Protocol | URL | Description |
| :--- | :--- | :--- | :--- |
| **🎮 Game WebSocket** | `WS` | `ws://192.168.0.114:8080/ws` | Real-time 30 FPS game stream & player inputs |
| **🔄 Reconnect WebSocket** | `WS` | `ws://192.168.0.114:8080/ws?token=<SESSION_TOKEN>` | Instant token-based reconnection & state catch-up |
| **📱 Emulator WebSocket** | `WS` | `ws://10.0.2.2:8080/ws` | Android Studio Emulator connection |
| **💻 Localhost WebSocket** | `WS` | `ws://localhost:8080/ws` | Local PC connection |
| **🛡️ Admin Telemetry** | `HTTP` | `http://192.168.0.114:8080/dashboard` | Multi-terminal live telemetry monitor |
| **🔐 Admin WS Stream** | `WS` | `ws://192.168.0.114:8080/ws/monitor?token=12345678` | Authenticated monitor socket (`Token: 12345678`) |
| **🟢 Health Check** | `HTTP GET` | `http://192.168.0.114:8080/health` | Server status & timestamp JSON |
| **ℹ️ Game Info** | `HTTP GET` | `http://192.168.0.114:8080/api/info` | Active game devices & arena metrics |

---

## 🧱 2. World Dimensions & Border Collision Specifications

| Metric / Parameter | Value | Description |
| :--- | :--- | :--- |
| **World Width** | `30000.0` | Total arena width |
| **World Height** | `30000.0` | Total arena height |
| **Border Thickness (Brick Margin)** | `220.0` | Outer brick border surrounding the arena |
| **Playable X Range** | `220.0` to `29780.0` | Safe playable horizontal coordinate range (`30000.0 - 220.0`) |
| **Playable Y Range** | `220.0` to `29780.0` | Safe playable vertical coordinate range (`30000.0 - 220.0`) |

### 💥 Border Collision Death Condition:
The server checks in each tick and direct location stream:
- `headX - headRadius <= 220.0`
- `headY - headRadius <= 220.0`
- `headX + headRadius >= 29780.0`
- `headY + headRadius >= 29780.0`

When touching or crossing this border, the snake **instantly dies**, bursts into food, and the server pushes `{"type": "game_over", "payload": {"status": "dead", "reason": "boundary_collision", ...}}`.

---

## 🔑 3. Token-Based Session Management & Reconnection

### 1. Game Start & Token Issuance
When the client starts a match, the server generates a unique 32-character session `token` and returns it in `started` and `joined` packets.
- **Client Action:** Store `token` in local storage / SQLite / SharedPreferences.

```json
{
  "type": "started",
  "payload": {
    "id": "p_1",
    "token": "tok_9f82d1c4b7204e19a4b3d76e28f1025a",
    "name": "PlayerName",
    "skin_id": 1,
    "spawn_x": 2500.0,
    "spawn_y": 3000.0,
    "angle": 1.57,
    "score": 0,
    "alive": true,
    "start": true,
    "world_w": 30000,
    "world_h": 30000,
    "tick_rate": 30,
    "timestamp": 1740000000000
  }
}
```

---

## 📡 3. Two-Step Disconnection Detection Architecture

To detect network loss and socket drops with minimal delay:

### 🔹 Step 1: Local Network Listener (OS Network Callbacks)
- Register Android `ConnectivityManager.NetworkCallback` / iOS Network Path Monitor.
- When internet drops (Wi-Fi off, Airplane mode, mobile network loss), **immediately trigger offline state** and stop trying until internet returns.
- When the OS notifies that internet connectivity is restored, **instantly initiate reconnection** using the saved `token`.

### 🔹 Step 2: Server Inactivity / Data Timeout
- If the socket remains open but no packets or pings arrive from the server for **4–5 seconds**, treat the connection as broken.
- Close the existing socket forcefully and initiate the reconnection workflow.

### 🔄 Reconnection Flow & Retry Policy:
1. **Immediate Retry with Exponential Backoff:**
   - 1st attempt: Immediate (0 ms)
   - 2nd attempt: 1.0s delay
   - 3rd attempt: 3.0s delay
2. **Offline Pause:** If 3 attempts fail and OS indicates no network, pause retrying.
3. **Instant Resume:** As soon as OS Network Callback fires network restored, immediately connect to `ws://HOST:8080/ws?token=<SAVED_TOKEN>` or send `{"type": "reconnect", "token": "<SAVED_TOKEN>"}`.

---

## 📦 4. Reconnection Responses (Server Authoritative)

### A. Living Snake Reconnected (`reconnect_success`)
If the snake is still alive in the arena, the server instantly sends the complete catch-up snapshot:
```json
{
  "type": "reconnect_success",
  "payload": {
    "id": "p_1",
    "token": "tok_9f82d1c4b7204e19a4b3d76e28f1025a",
    "name": "PlayerName",
    "spawn_x": 2840.5,
    "spawn_y": 3120.0,
    "angle": 1.57,
    "score": 140,
    "alive": true,
    "boost": false,
    "skin_id": 1,
    "body": [{"x": 2828.5, "y": 3120.0}, {"x": 2816.5, "y": 3120.0}],
    "world_w": 30000,
    "world_h": 30000,
    "tick_rate": 30,
    "timestamp": 1740000015000
  }
}
```
*Client snaps its rendering to these authoritative coordinates and resumes normal gameplay.*

### B. Snake Died While Offline (`game_over`)
If the snake died while the client was disconnected (e.g., collided with a wall or another snake):
```json
{
  "type": "game_over",
  "payload": {
    "id": "p_1",
    "token": "tok_9f82d1c4b7204e19a4b3d76e28f1025a",
    "status": "dead",
    "reason": "left_or_defeated",
    "final_score": 140,
    "died_at": 1740000012000
  }
}
```
*Client clears local token and displays Game Over / Results screen.*

---

## ⚡ 5. Network Protocol Specification

All binary numbers are encoded in **Little-Endian** format.

### 📤 A. Client -> Server Messages

#### 1. Start Game / Join Game (JSON sent when user clicks "Start Game")
Connecting to the WebSocket establishes the link (`welcome` packet received), but **the game/snake will NOT start until the client explicitly sends a start call**.

* **Single-line String to send over WebSocket when Start Game is pressed:**
```json
{"type": "start", "payload": {"start": true, "name": "PlayerName", "skin_id": 1}}
```
*(Or compact: `{"type":"start","start":true,"name":"PlayerName"}` or `{"type":"join","payload":{"start":true}}`)*

* **Server replies with `started` (and `joined`) confirmation packet:**
```json
{
  "type": "started",
  "payload": {
    "start": true,
    "id": "p_1",
    "name": "PlayerName",
    "skin_id": 1,
    "spawn_x": 2500.0,
    "spawn_y": 3000.0,
    "angle": 1.57,
    "score": 0,
    "alive": true,
    "world_w": 30000,
    "world_h": 30000,
    "tick_rate": 30,
    "timestamp": 1740000000000
  }
}
```

#### 2. Defeat / Player Die / Reset / Leave (JSON on Game Over)
When the player dies or leaves, client can send:
```json
{
  "type": "player_die"
}
```
*(Aliases: `"defeat"`, `"die"`, `"leave"`, `"default"`, `"reset"`)*
*Server replies with `player_die_ack` and player can immediately send `join` or `respawn` on the same connection to play again.*

#### 2. Snake Location Stream (Binary - 14 Bytes, Real-Time / 30-60 FPS)
* Client only sends its snake coordinates. The server authoritatively calculates food collisions when the snake passes over static foods.
* **Format:**
  - `[Byte 0]` : `0x07` (Opcode: `BinOpLocation`)
  - `[Bytes 1..4]` : `Float32` (Head X Position, Little-Endian)
  - `[Bytes 5..8]` : `Float32` (Head Y Position, Little-Endian)
  - `[Bytes 9..12]` : `Float32` (Angle in Radians, Little-Endian)
  - `[Byte 13]` : `Uint8` (`1` = Boosting, `0` = Normal)

* **JSON Alternative:**
```json
{
  "type": "location",
  "payload": {
    "x": 1500.5,
    "y": 2400.0,
    "angle": 1.57,
    "boost": false
  }
}
```

#### 3. Steering & Boost Input (Binary - 6 Bytes, Optional)
* **Format:**
  - `[Byte 0]` : `0x02` (Opcode: `BinOpInput`)
  - `[Bytes 1..4]` : `Float32` (Angle in Radians, Little-Endian)
  - `[Byte 5]` : `Uint8` (`1` = Boost Active, `0` = Normal)

#### 4. Ultra-Fast Binary Ping / Pong (RTT Measurement - 9 Bytes Total)
To accurately display network Ping (ms) in real-time with zero server allocation:
* **Client -> Server Ping Packet (9 Bytes):**
  - `[Byte 0]` : `0x08` (Opcode: `OP_PING`)
  - `[Bytes 1..8]` : `Int64` (Client Timestamp `t` in milliseconds, Little-Endian)
* **Server -> Client Pong Packet (9 Bytes - Direct Echo):**
  - `[Byte 0]` : `0x08` (Opcode: `OP_PONG`)
  - `[Bytes 1..8]` : `Int64` (Echoed Client Timestamp `t`, Little-Endian)
* **Client Ping Calculation:** `RTT = System.currentTimeMillis() - echoedTimestamp`.
* **Rate Limiting:** Server rate-limits ping echoes to 1 response per 500ms (max 2 pings/sec per client).
* **Heartbeat & Timeout:** Keeps connection alive. Inactive connections (> 5s without any packet/ping) are automatically cleaned up.

#### 5. Location Sync Request (Lag Recovery / Dead Reckoning Sync)
When a client experiences network lag or disconnects temporarily:
- The **Server is 100% Authoritative**: In the server's 30 TPS simulation loop, the snake continues moving forward along its last known angle/speed (Dead Reckoning). Any foods eaten or boundary collisions during this time are authoritatively resolved on the server.
- When connection resumes, client requests sync via JSON `{"type": "sync"}` or Binary `[0x09]` (or in Ping response).
- **Server Response (`location_sync` / Binary `0x09`):**
```json
{
  "type": "location_sync",
  "payload": {
    "id": "p_1",
    "name": "PlayerName",
    "x": 2150.4,
    "y": 1820.0,
    "angle": 1.57,
    "score": 45,
    "alive": true,
    "boost": false,
    "body": [{"x": 2138.4, "y": 1820.0}, ...]
  }
}
```
* **Binary Response Format (`0x09`):**
  - `[Byte 0]` : `0x09` (`BinOpLocationSync`)
  - `[Bytes 1..4]` : `Float32` (Authoritative Head X)
  - `[Bytes 5..8]` : `Float32` (Authoritative Head Y)
  - `[Bytes 9..12]` : `Float32` (Authoritative Angle)
  - `[Bytes 13..16]` : `Int32` (Authoritative Score)
  - `[Byte 17]` : `Uint8` (Bit 0: Alive, Bit 1: Boost)
  - `[Bytes 18..19]` : `Uint16` (Body Segments Count `S`)
  - For each of the `S` segments: `[4B Float32 SegX][4B Float32 SegY]`

---

### 📥 B. Server -> Client Messages

#### 1. Real-Time Eaten Food Broadcast (Binary: `Opcode 0x06` & JSON: `food_eaten`)
Broadcast immediately to **ALL connected clients** whenever a snake passes over a static food orb on the server.
Handles 150+ concurrent players with instant 0-latency.

* **Binary Format (`0x06`):**
  - `[Byte 0]` : `0x06` (Opcode: `BinOpEatBatch`)
  - `[Bytes 1..2]` : `Uint16` (Eaten Items Count `E`, Little-Endian)
  - For each of the `E` items:
    - `[4 Bytes]` : `Uint32` (Food ID / Number)
    - `[8 Bytes]` : `ASCII String` (Eater Player ID)
    - `[4 Bytes]` : `Int32` (Eater's New Total Score)

* **JSON Format:**
```json
{
  "type": "food_eaten",
  "payload": {
    "food_id": 1420,
    "eater_id": "p_1",
    "score": 105,
    "color": 3
  }
}
```

* **Client Action upon receiving eaten food:**
  1. Remove `food_id` / Food Number from local canvas/map rendering.
  2. Update `eater_id`'s snake score and target length on screen.

#### 2. Server-Authoritative Instant Death Notification (`game_over` / `player_die`)
When a snake dies on the server due to **Boundary Collision** or **Snake-to-Snake Body/Head Collision**, the server **immediately pushes a JSON death notice to the client**:

* **Packet received by dying client:**
```json
{
  "type": "game_over",
  "payload": {
    "id": "p_1",
    "token": "tok_9f82d1c4b7204e19a4b3d76e28f1025a",
    "status": "dead",
    "reason": "boundary_collision",
    "final_score": 140,
    "died_at": 1740000012000
  }
}
```
*(Also sent with `"type": "player_die"` for backwards compatibility)*

* **Broadcast to all other clients:**
```json
{
  "type": "player_died",
  "payload": {
    "id": "p_1",
    "status": "dead",
    "reason": "collided_with_PlayerTwo",
    "final_score": 140,
    "died_at": 1740000012000
  }
}
```

* **Client Action upon receiving `game_over`:**
  1. Trigger local game over / death animation.
  2. Display final score and Death Reason.
  3. Show Play Again / Respawn button (client can send `{"type": "start", "payload": {"start": true}}` on the same connection).

#### 3. Unified 30 FPS WorldState Frame (Binary: `Opcode 0x01`)
Broadcast to all clients every ~33ms (30 TPS).

* **Header:**
  - `[Byte 0]` : `0x01` (Opcode)
  - `[Bytes 1..8]` : `Int64` (Server Timestamp in ms)

* **Block 1: Players (Snakes):**
  - `[2 Bytes]` : `Uint16` (Player Count `N`)
  - For each of the `N` players:
    - `[8 Bytes]` : `ASCII String` (Player ID, padded with spaces/nulls)
    - `[4 Bytes]` : `Float32` (Head X position)
    - `[4 Bytes]` : `Float32` (Head Y position)
    - `[4 Bytes]` : `Float32` (Angle in radians)
    - `[4 Bytes]` : `Int32` (Current Score)
    - `[1 Byte]`  : `Flags` (Bit 0 = Alive, Bit 1 = Boosting)
    - `[2 Bytes]` : `Uint16` (Body Segment Count `S`)
    - For each of the `S` segments:
      - `[4 Bytes]` : `Float32` (Segment X)
      - `[4 Bytes]` : `Float32` (Segment Y)

* **Block 2: Batched Eaten Foods (All foods claimed in this 33ms window):**
  - `[2 Bytes]` : `Uint16` (Eaten Count `E`)
  - For each of the `E` eaten items:
    - `[4 Bytes]` : `Uint32` (Food ID)
    - `[8 Bytes]` : `ASCII String` (Eater Player ID)
    - `[4 Bytes]` : `Int32` (Eater's New Total Score)

* **Block 3: Batched Spawned / Death Foods:**
  - `[2 Bytes]` : `Uint16` (Spawned Count `F`)
  - For each of the `F` spawned items:
    - `[4 Bytes]` : `Uint32` (Food ID)
    - `[1 Byte]`  : `Uint8` (Color / Code index 0..7)
    - `[4 Bytes]` : `Float32` (X Position)
    - `[4 Bytes]` : `Float32` (Y Position)
    - `[2 Bytes]` : `Uint16` (Food Value)

---

## 🤖 3. Ready-to-Use Client Integration Prompt

Copy the prompt below to generate or integrate the Kotlin/Android client code:

```text
Please integrate the WebSocket networking client in our Android Kotlin app (SnakeSlitherPro) with the following authoritative server specifications:

1. Server WebSocket URL:
   - Physical Device / WiFi: "ws://192.168.0.114:8080/ws"
   - Android Emulator: "ws://10.0.2.2:8080/ws"
   - Localhost: "ws://localhost:8080/ws"

2. Initial Handshake:
   - On WebSocket open, send JSON Join packet:
     {"type": "join", "payload": {"name": "HeroSnake", "skin_id": 1}}

3. Real-Time Snake Location Streaming (Binary Opcode 0x07 - 14 Bytes Little-Endian):
   - Client sends its snake position:
     ByteBuffer: [0x07: Byte][HeadX: Float32][HeadY: Float32][Angle: Float32][Boost: 1 or 0 Byte]
   - NOTE: Client does NOT calculate or send food eat events. Server authoritatively computes collision with static foods.

4. Receiving Real-Time Eaten Food Notification (Binary Opcode 0x06):
   - Format: [0x06: Byte][Count: Short16] -> Loop [FoodID: Int32][EaterID: 8 Bytes ASCII][NewScore: Int32]
   - Action: Immediately remove FoodID from canvas rendering, and update player score & length.

5. Receiving 30 FPS Unified Frame (Binary Opcode 0x01):
   - Header: [0x01][Timestamp: Long]
   - Block 1: Players Count (Short) -> Loop [8B ID, 4B HeadX, 4B HeadY, 4B Angle, 4B Score, 1B Flags, SegmentCount: Short -> Loop (4B SegX, 4B SegY)]
   - Block 2: Eaten Foods Frame Batch (Short) -> Loop [4B FoodID, 8B EaterID, 4B NewScore]
   - Block 3: Spawned Foods Batch (Short) -> Loop [4B FoodID, 1B ColorIndex, 4B X, 4B Y, 2B Value]

Please implement using OkHttp WebSocketListener in Kotlin with proper Little-Endian ByteBuffers and thread-safe UI updates.
```
