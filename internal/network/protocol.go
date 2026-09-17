package network

import (
	"bytes"
	"encoding/binary"
	"math"
	"snake_game_server/internal/game"
)

// Opcode constants for network communication
const (
	OpJoin             = "join"
	OpJoined           = "joined"
	OpStart            = "start"
	OpStartGame        = "start_game"
	OpStarted          = "started"
	OpRespawn          = "respawn"
	OpReconnect        = "reconnect"
	OpReconnectSuccess = "reconnect_success"
	OpGameOver         = "game_over"
	OpInput            = "input"
	OpWorldState       = "world_state"
	OpPlayerDie        = "player_die"
	OpLeave            = "leave"
	OpPing             = "ping"
	OpPong             = "pong"
	OpSync             = "sync"
	OpLocationSync     = "location_sync"
	OpChat             = "chat"
	OpFoodRelay        = "food"
	OpLocation         = "location"
	OpStoreSync        = "store_sync"
)

// Binary Opcode bytes
const (
	BinOpWorldState    byte = 0x01 // 30 TPS Unified Frame (Players + Eaten Frame Batch + Spawned Frame Batch)
	BinOpInput         byte = 0x02 // Steering & Boost input
	BinOpPing          byte = 0x03
	BinOpPong          byte = 0x04
	BinOpFoodRelay     byte = 0x05 // Pure Binary Food Data Relay
	BinOpEatBatch      byte = 0x06 // Binary Eaten Food Action / Frame Batch Relay
	BinOpLocation      byte = 0x07 // Direct authoritative client location stream
	BinOpLocationSync  byte = 0x08 // Authoritative server position sync (dead reckoning recovery)
)

// ChatPayload represents real-time chat message broadcast
type ChatPayload struct {
	SenderID  string `json:"sender_id"`
	Name      string `json:"name"`
	Message   string `json:"message"`
	Timestamp int64  `json:"ts"`
}

// BaseMessage represents standard incoming and outgoing payload container
type BaseMessage struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload,omitempty"`
}

// JoinPayload (or StartPayload) is sent by client when user clicks "Start Game" or respawns
type JoinPayload struct {
	ID       string `json:"id,omitempty"`
	PlayerID string `json:"player_id,omitempty"`
	Token    string `json:"token,omitempty"`
	Name     string `json:"name,omitempty"`
	SkinID   int    `json:"skin_id,omitempty"`
	Start    bool   `json:"start,omitempty"`
}

// StartPayload alias for JoinPayload
type StartPayload = JoinPayload

// ReconnectPayload is sent by client to resume an existing session using its token
type ReconnectPayload struct {
	Token    string `json:"token"`
	ID       string `json:"id,omitempty"`
	PlayerID string `json:"player_id,omitempty"`
}

// GameOverPayload informs client that the snake has died
type GameOverPayload struct {
	ID         string `json:"id"`
	Token      string `json:"token,omitempty"`
	Reason     string `json:"reason"`
	FinalScore int    `json:"final_score"`
	DiedAt     int64  `json:"died_at"`
	Status     string `json:"status"` // "dead"
}

// InputPayload is sent by client to update steering and boost
type InputPayload struct {
	TargetAngle float64 `json:"angle"`
	IsBoosting  bool    `json:"boost"`
	Seq         uint32  `json:"seq"`
}

// StoreSyncPayload represents periodic (30s) score, segment, and food count sync
type StoreSyncPayload struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Score            int    `json:"score"`
	Segments         int    `json:"segments"`
	StaticFoodsEaten int    `json:"static_foods_eaten"`
	Timestamp        int64  `json:"ts"`
}

// EncodeEatBatchBinary packs a batch of accepted eaten foods into a dedicated binary block (Server Broadcast to Clients):
// [1B: 0x06 (BinOpEatBatch)]
// [2B: Count uint16]
// Per Item:
//   - [4B: FoodID uint32]
//   - [8B: EaterID string 8B]
//   - [4B: EaterNewScore int32]
func EncodeEatBatchBinary(events []game.FoodEatenEvent) []byte {
	buf := new(bytes.Buffer)
	buf.WriteByte(BinOpEatBatch)

	count := uint16(len(events))
	binary.Write(buf, binary.LittleEndian, count)

	for _, e := range events {
		binary.Write(buf, binary.LittleEndian, e.FoodID)

		var idBytes [8]byte
		copy(idBytes[:], e.EaterID)
		buf.Write(idBytes[:])

		binary.Write(buf, binary.LittleEndian, int32(e.NewScore))
	}
	return buf.Bytes()
}

// EncodeWorldStateBinary packs the complete 30 TPS frame into a single unified binary block:
// [1B: Opcode 0x01]
// [8B: Timestamp int64]
//
// --- BLOCK 1: PLAYERS / SNAKES ---
// [2B: Player Count uint16]
// Per Player:
//   - [8B: Player ID]
//   - [4B: Head X float32]
//   - [4B: Head Y float32]
//   - [4B: Angle float32]
//   - [4B: Score int32]
//   - [1B: Flags (Bit 0: alive, Bit 1: boost)]
//   - [2B: Segment Count uint16]
//   - Per Segment: [4B SegX float32][4B SegY float32]
//
// --- BLOCK 2: BATCHED EATEN FOODS (All foods claimed and eaten across all players/bots in this 33ms frame) ---
// [2B: Eaten Count uint16]
// Per Eaten Item:
//   - [4B: FoodID uint32]
//   - [8B: EaterID string 8B]
//   - [4B: EaterNewScore int32]
//
// --- BLOCK 3: BATCHED SPAWNED FOODS (Foods spawned/dropped in this 33ms frame) ---
// [2B: Spawned Count uint16]
// Per Spawned Item:
//   - [4B: FoodID uint32]
//   - [1B: ColorCode uint8]
//   - [4B: X float32]
//   - [4B: Y float32]
//   - [2B: Value uint16]
func EncodeWorldStateBinary(state *game.WorldState) []byte {
	buf := new(bytes.Buffer)

	// Opcode & Timestamp
	buf.WriteByte(BinOpWorldState)
	binary.Write(buf, binary.LittleEndian, state.Timestamp)

	// --- BLOCK 1: PLAYERS ---
	playerCount := uint16(len(state.Players))
	binary.Write(buf, binary.LittleEndian, playerCount)

	for _, p := range state.Players {
		var idBytes [8]byte
		copy(idBytes[:], p.ID)
		buf.Write(idBytes[:])

		binary.Write(buf, binary.LittleEndian, float32(p.Head.X))
		binary.Write(buf, binary.LittleEndian, float32(p.Head.Y))
		binary.Write(buf, binary.LittleEndian, float32(p.Angle))
		binary.Write(buf, binary.LittleEndian, int32(p.Score))

		var flags byte = 0
		if p.IsAlive {
			flags |= 1 << 0
		}
		if p.IsBoost {
			flags |= 1 << 1
		}
		buf.WriteByte(flags)

		segCount := uint16(len(p.Body))
		binary.Write(buf, binary.LittleEndian, segCount)

		for _, seg := range p.Body {
			binary.Write(buf, binary.LittleEndian, float32(seg.X))
			binary.Write(buf, binary.LittleEndian, float32(seg.Y))
		}
	}

	// --- BLOCK 2: EATEN FOODS FRAME BATCH ---
	eatenCount := uint16(len(state.EatenFoods))
	binary.Write(buf, binary.LittleEndian, eatenCount)

	for _, ef := range state.EatenFoods {
		binary.Write(buf, binary.LittleEndian, ef.FoodID)

		var eaterBytes [8]byte
		copy(eaterBytes[:], ef.EaterID)
		buf.Write(eaterBytes[:])

		binary.Write(buf, binary.LittleEndian, int32(ef.NewScore))
	}

	// --- BLOCK 3: SPAWNED FOODS FRAME BATCH ---
	spawnedCount := uint16(len(state.SpawnedFoods))
	binary.Write(buf, binary.LittleEndian, spawnedCount)

	for _, sf := range state.SpawnedFoods {
		binary.Write(buf, binary.LittleEndian, sf.FoodID)
		buf.WriteByte(sf.ColorIndex)
		binary.Write(buf, binary.LittleEndian, sf.X)
		binary.Write(buf, binary.LittleEndian, sf.Y)
		binary.Write(buf, binary.LittleEndian, sf.Value)
	}

	return buf.Bytes()
}

// DecodeInputBinary unpacks incoming binary input block: [Opcode 1B][Angle float32 4B][Boost 1B]
func DecodeInputBinary(data []byte) (angle float64, isBoost bool, ok bool) {
	if len(data) < 6 || data[0] != BinOpInput {
		return 0, false, false
	}
	bits := binary.LittleEndian.Uint32(data[1:5])
	angle32 := math.Float32frombits(bits)
	boost := (data[5] == 1)
	return float64(angle32), boost, true
}

// LocationPayload represents direct client position stream at custom FPS
type LocationPayload struct {
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Angle      float64 `json:"angle"`
	IsBoosting bool    `json:"boost"`
}

// DecodeLocationBinary unpacks: [Opcode 0x07][X float32 4B][Y float32 4B][Angle float32 4B][Boost 1B]
func DecodeLocationBinary(data []byte) (x, y, angle float64, isBoost bool, ok bool) {
	if len(data) < 14 || data[0] != BinOpLocation {
		return 0, 0, 0, false, false
	}
	xBits := binary.LittleEndian.Uint32(data[1:5])
	yBits := binary.LittleEndian.Uint32(data[5:9])
	angBits := binary.LittleEndian.Uint32(data[9:13])

	x = float64(math.Float32frombits(xBits))
	y = float64(math.Float32frombits(yBits))
	angle = float64(math.Float32frombits(angBits))
	isBoost = (data[13] == 1)
	return x, y, angle, isBoost, true
}

// EncodeLocationSyncBinary encodes authoritative position & segments for a player:
// [1B: Opcode 0x08]
// [4B: Head X float32]
// [4B: Head Y float32]
// [4B: Angle float32]
// [4B: Score int32]
// [1B: Flags (Bit 0: alive, Bit 1: boost)]
// [2B: Segments count uint16]
// Per Segment: [4B: SegX float32][4B: SegY float32]
func EncodeLocationSyncBinary(p *game.PlayerDTO) []byte {
	buf := new(bytes.Buffer)
	buf.WriteByte(BinOpLocationSync)
	binary.Write(buf, binary.LittleEndian, float32(p.Head.X))
	binary.Write(buf, binary.LittleEndian, float32(p.Head.Y))
	binary.Write(buf, binary.LittleEndian, float32(p.Angle))
	binary.Write(buf, binary.LittleEndian, int32(p.Score))

	var flags byte = 0
	if p.IsAlive {
		flags |= 1 << 0
	}
	if p.IsBoost {
		flags |= 1 << 1
	}
	buf.WriteByte(flags)

	segCount := uint16(len(p.Body))
	binary.Write(buf, binary.LittleEndian, segCount)

	for _, seg := range p.Body {
		binary.Write(buf, binary.LittleEndian, float32(seg.X))
		binary.Write(buf, binary.LittleEndian, float32(seg.Y))
	}
	return buf.Bytes()
}


