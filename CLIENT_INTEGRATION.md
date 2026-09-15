# 🚀 Snake Slither Pro - Server Integration & Protocol Guide

This document contains the complete server endpoints, connection URLs, binary network protocol specifications, and a ready-to-use AI/Developer Prompt for integrating the Android (Kotlin) client.

---

## 🌐 1. Server Endpoints & URLs

| Service / Endpoint | Protocol | URL | Description |
| :--- | :--- | :--- | :--- |
| **🎮 Game WebSocket** | `WS` | `ws://192.168.0.114:8080/ws` | Real-time 30 FPS game stream & player inputs |
| **📱 Emulator WebSocket** | `WS` | `ws://10.0.2.2:8080/ws` | Android Studio Emulator connection |
| **💻 Localhost WebSocket** | `WS` | `ws://localhost:8080/ws` | Local PC connection |
| **🛡️ Admin Telemetry** | `HTTP` | `http://192.168.0.114:8080/dashboard` | Multi-terminal live telemetry monitor |
| **🔐 Admin WS Stream** | `WS` | `ws://192.168.0.114:8080/ws/monitor?token=12345678` | Authenticated monitor socket (`Token: 12345678`) |
| **🟢 Health Check** | `HTTP GET` | `http://192.168.0.114:8080/health` | Server status & timestamp JSON |
| **ℹ️ Game Info** | `HTTP GET` | `http://192.168.0.114:8080/api/info` | Active game devices & arena metrics |

---

## ⚡ 2. Network Protocol Specification

All binary numbers are encoded in **Little-Endian** format.

### 📤 A. Client -> Server Messages

#### 1. Join Game (JSON upon connecting)
```json
{
  "type": "join",
  "payload": {
    "name": "PlayerName",
    "skin_id": 1
  }
}
```

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

#### 2. Unified 30 FPS WorldState Frame (Binary: `Opcode 0x01`)
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
