package network

import (
	"bytes"
	"encoding/binary"
	"math"
	"snake_game_server/internal/game"
)

// Opcode constants for network communication
const (
	OpJoin       = "join"
	OpInput      = "input"
	OpWorldState = "world_state"
	OpPlayerDie  = "player_die"
	OpPing       = "ping"
	OpPong       = "pong"
	OpChat       = "chat"
	OpEatFood    = "eat_food"
	OpFoodRelay  = "food"
)

// Binary Opcode bytes
const (
	BinOpWorldState byte = 0x01 // 30 TPS Unified Frame (Players + Eaten Frame Batch + Spawned Frame Batch)
	BinOpInput      byte = 0x02 // Steering & Boost input
	BinOpPing       byte = 0x03
	BinOpPong       byte = 0x04
	BinOpFoodRelay  byte = 0x05 // Pure Binary Food Data Relay
	BinOpEatBatch   byte = 0x06 // Binary Eaten Food Action / Frame Batch Relay
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

// JoinPayload is sent by client upon establishing connection
type JoinPayload struct {
	Name   string `json:"name"`
	SkinID int    `json:"skin_id"`
}

// InputPayload is sent by client to update steering and boost
type InputPayload struct {
	TargetAngle float64 `json:"angle"`
	IsBoosting  bool    `json:"boost"`
	Seq         uint32  `json:"seq"`
}

// EatFoodPayload is sent when a client eats a food item (JSON mode)
type EatFoodPayload struct {
	FoodID uint32 `json:"id"`
	Value  int    `json:"val,omitempty"`
}

// ClientEatItem represents a single food eat item sent by client
type ClientEatItem struct {
	FoodID    uint32
	ScoreGain int
}

// DecodeEatFoodBinary unpacks client eat frame(s):
// Supports single eat: [0x06][4B FoodID][2B ScoreGain]
// Or batched eats: [0x06][0xFF 0xFF marker][2B Count] + Per Item [4B FoodID][2B ScoreGain]
func DecodeEatFoodBinary(data []byte) (items []ClientEatItem, ok bool) {
	if len(data) < 5 || data[0] != BinOpEatBatch {
		return nil, false
	}

	// Batched eat payload
	if len(data) >= 7 && data[1] == 0xFF && data[2] == 0xFF {
		count := int(binary.LittleEndian.Uint16(data[3:5]))
		items = make([]ClientEatItem, 0, count)
		offset := 5
		for i := 0; i < count && offset+6 <= len(data); i++ {
			fid := binary.LittleEndian.Uint32(data[offset : offset+4])
			gain := int(binary.LittleEndian.Uint16(data[offset+4 : offset+6]))
			if gain <= 0 {
				gain = 1
			}
			items = append(items, ClientEatItem{FoodID: fid, ScoreGain: gain})
			offset += 6
		}
		return items, len(items) > 0
	}

	// Single eat payload
	fid := binary.LittleEndian.Uint32(data[1:5])
	gain := 1
	if len(data) >= 7 {
		gain = int(binary.LittleEndian.Uint16(data[5:7]))
		if gain <= 0 {
			gain = 1
		}
	}
	return []ClientEatItem{{FoodID: fid, ScoreGain: gain}}, true
}

// EncodeEatBatchBinary packs a batch of accepted eaten foods into a dedicated binary block:
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
