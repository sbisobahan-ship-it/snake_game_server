package game

import (
	"snake_game_server/internal/physics"
	"time"
)

// Food represents an active food orb in the arena
type Food struct {
	ID          uint32           `json:"id"`
	ColorIndex  uint8            `json:"c"`
	Pos         physics.Vector2D `json:"pos"`
	Value       int              `json:"val"`
	Radius      float64          `json:"r"`
	IsFromDeath bool             `json:"from_death"`
	CreatedAt   int64            `json:"created_at"`
}

// FoodDTO is a lightweight data transfer object for food items
type FoodDTO struct {
	ID          uint32  `json:"id"`
	ColorIndex  uint8   `json:"c"`
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
	Val         int     `json:"v"`
	Radius      float64 `json:"r"`
	IsFromDeath bool    `json:"d,omitempty"`
}

// ToDTO converts Food to compact FoodDTO
func (f *Food) ToDTO() FoodDTO {
	return FoodDTO{
		ID:          f.ID,
		ColorIndex:  f.ColorIndex,
		X:           f.Pos.X,
		Y:           f.Pos.Y,
		Val:         f.Value,
		Radius:      f.Radius,
		IsFromDeath: f.IsFromDeath,
	}
}

// NewFood creates a simple food entity
func NewFood(id uint32, pos physics.Vector2D, val int, radius float64, colorIndex uint8, isFromDeath bool) *Food {
	if radius <= 0 {
		radius = 8.0
	}
	if val <= 0 {
		val = 1
	}

	return &Food{
		ID:          id,
		ColorIndex:  colorIndex,
		Pos:         pos,
		Value:       val,
		Radius:      radius,
		IsFromDeath: isFromDeath,
		CreatedAt:   time.Now().UnixMilli(),
	}
}
