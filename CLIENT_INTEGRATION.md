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

#### 2. Steering & Boost Input (Binary - 6 Bytes)
* Sent at touch/joystick move rate (or 30/60 Hz).
* **Format:**
  - `[Byte 0]` : `0x02` (Opcode: `BinOpInput`)
  - `[Bytes 1..4]` : `Float32` (Angle in Radians, Little-Endian)
  - `[Byte 5]` : `Uint8` (`1` = Boost Active, `0` = Normal)

#### 3. Eat Food Action (Binary - 7 Bytes)
* Sent when player snake head touches a food orb.
* **Format:**
  - `[Byte 0]` : `0x06` (Opcode: `BinOpEatBatch`)
  - `[Bytes 1..4]` : `Uint32` (Food ID, Little-Endian)
  - `[Bytes 5..6]` : `Uint16` (Score Gain / Value, Little-Endian)

#### 4. Generic Binary Food Relay (Binary)
* Broadcasts raw binary food code/action to all other players.
* **Format:**
  - `[Byte 0]` : `0x05` (Opcode: `BinOpFoodRelay`)
  - `[Bytes 1..N]` : `[Raw Binary Food Payload...]`

---

### 📥 B. Server -> Client Messages

#### 1. Unified 30 FPS WorldState Frame (Binary: `Opcode 0x01`)
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
  - *Client Action:* Remove `Food ID` from local canvas and update scores.

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
Please integrate the WebSocket networking client in our Android Kotlin app (SnakeSlitherPro) with the following specifications:

1. Server WebSocket URL:
   - Physical Device / WiFi: "ws://192.168.0.114:8080/ws"
   - Android Emulator: "ws://10.0.2.2:8080/ws"

2. Initial Handshake:
   - On WebSocket open, send JSON Join packet:
     {"type": "join", "payload": {"name": "HeroSnake", "skin_id": 1}}

3. Real-Time Inputs (Binary):
   - Steering & Boost (Opcode 0x02, 6 Bytes Little-Endian):
     ByteBuffer: [0x02: Byte][Angle: Float32][Boost: 1 or 0 Byte]
   
   - Food Eat Action (Opcode 0x06, 7 Bytes Little-Endian):
     ByteBuffer: [0x06: Byte][FoodID: Int32][ScoreGain: Short16]

4. Receiving 30 FPS Unified Frame (Binary Opcode 0x01):
   - Header: [0x01][Timestamp: Long]
   - Block 1: Players Count (Short) -> Loop [8B ID, 4B HeadX, 4B HeadY, 4B Angle, 4B Score, 1B Flags, SegmentCount: Short -> Loop (4B SegX, 4B SegY)]
   - Block 2: Eaten Foods Frame Batch (Short) -> Loop [4B FoodID, 8B EaterID, 4B NewScore] -> Remove FoodID from local rendering.
   - Block 3: Spawned Foods Batch (Short) -> Loop [4B FoodID, 1B ColorIndex, 4B X, 4B Y, 2B Value] -> Add to local food rendering list.

Please implement using OkHttp WebSocketListener in Kotlin with proper Little-Endian ByteBuffers and thread-safe UI updates.
```
