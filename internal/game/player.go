package game

import (
	"snake_game_server/internal/physics"
)

// Player represents a connected participant in the game
type Player struct {
	ID             string `json:"id"`
	Token          string `json:"token"`
	Name           string `json:"name"`
	Snake          *Snake `json:"snake"`
	IsReady        bool   `json:"is_ready"`
	IsConnected    bool   `json:"is_connected"`
	DisconnectedAt int64  `json:"disconnected_at,omitempty"`
	DeathReason    string `json:"death_reason,omitempty"`
	DiedAt         int64  `json:"died_at,omitempty"`
	Ping           int64  `json:"ping"`
}

// DeadSession stores terminal state of a player session after death for reconnect rejection
type DeadSession struct {
	Token       string `json:"token"`
	PlayerID    string `json:"player_id"`
	Name        string `json:"name"`
	FinalScore  int    `json:"final_score"`
	DeathReason string `json:"death_reason"`
	DiedAt      int64  `json:"died_at"`
}

// NewPlayer creates a new player instance with a spawned snake
func NewPlayer(id, token, name string, spawnPos physics.Vector2D, angle float64, skinID int) *Player {
	return &Player{
		ID:             id,
		Token:          token,
		Name:           name,
		Snake:          NewSnake(id, spawnPos, angle, skinID),
		IsReady:        true,
		IsConnected:    true,
		DisconnectedAt: 0,
		Ping:           0,
	}
}
