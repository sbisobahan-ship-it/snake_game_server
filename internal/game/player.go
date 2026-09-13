package game

import (
	"snake_game_server/internal/physics"
)

// Player represents a connected participant in the game
type Player struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Snake    *Snake `json:"snake"`
	IsReady  bool   `json:"is_ready"`
	Ping     int64  `json:"ping"`
}

// NewPlayer creates a new player instance with a spawned snake
func NewPlayer(id, name string, spawnPos physics.Vector2D, angle float64, skinID int) *Player {
	return &Player{
		ID:      id,
		Name:    name,
		Snake:   NewSnake(id, spawnPos, angle, skinID),
		IsReady: true,
		Ping:    0,
	}
}
