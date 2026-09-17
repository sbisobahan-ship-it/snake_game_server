package game

import (
	"math"
	"snake_game_server/internal/physics"
)

const (
	DefaultBaseSpeed  = 200.0 // Units per second
	DefaultBoostSpeed = 380.0 // Units per second
	DefaultTurnSpeed  = 4.5   // Radians per second
	SegmentSpacing    = 12.0  // Distance between body segments
	DefaultHeadRadius = 14.0
	DefaultBodyRadius = 12.0
	InitialSegments   = 12
)

// Snake represents a player's snake entity in the game
type Snake struct {
	ID           string             `json:"id"`
	Head         physics.Vector2D   `json:"head"`
	Angle        float64            `json:"angle"`        // Current direction angle
	TargetAngle  float64            `json:"target_angle"` // Target angle from client input
	Speed        float64            `json:"speed"`
	BaseSpeed    float64            `json:"base_speed"`
	BoostSpeed   float64            `json:"boost_speed"`
	IsBoosting   bool               `json:"is_boosting"`
	IsAlive      bool               `json:"is_alive"`
	Body         []physics.Vector2D `json:"body"`   // Ordered list of segment positions
	HeadRadius   float64            `json:"head_r"` // Radius for collision detection
	BodyRadius   float64            `json:"body_r"`
	Score            int                `json:"score"`
	SkinID           int                `json:"skin_id"`
	TargetLength     int                `json:"target_length"`
	StaticFoodsEaten int                `json:"static_foods_eaten"`
}

// NewSnake instantiates a fresh snake entity at spawn position
func NewSnake(id string, spawnPos physics.Vector2D, angle float64, skinID int) *Snake {
	body := make([]physics.Vector2D, InitialSegments)
	for i := 0; i < InitialSegments; i++ {
		// Spawn segments trailing behind the head
		body[i] = physics.Vector2D{
			X: spawnPos.X - math.Cos(angle)*float64(i+1)*SegmentSpacing,
			Y: spawnPos.Y - math.Sin(angle)*float64(i+1)*SegmentSpacing,
		}
	}

	return &Snake{
		ID:               id,
		Head:             spawnPos,
		Angle:            angle,
		TargetAngle:      angle,
		Speed:            DefaultBaseSpeed,
		BaseSpeed:        DefaultBaseSpeed,
		BoostSpeed:       DefaultBoostSpeed,
		IsBoosting:       false,
		IsAlive:          true,
		Body:             body,
		HeadRadius:       DefaultHeadRadius,
		BodyRadius:       DefaultBodyRadius,
		Score:            0,
		SkinID:           skinID,
		TargetLength:     InitialSegments,
		StaticFoodsEaten: 0,
	}
}

// UpdateDirection smoothly turns snake toward target angle given delta time
func (s *Snake) UpdateDirection(dt float64) {
	diff := physics.NormalizeAngle(s.TargetAngle - s.Angle)
	maxTurn := DefaultTurnSpeed * dt

	if math.Abs(diff) <= maxTurn {
		s.Angle = s.TargetAngle
	} else if diff > 0 {
		s.Angle += maxTurn
	} else {
		s.Angle -= maxTurn
	}
	s.Angle = physics.NormalizeAngle(s.Angle)
}

// UpdatePosition updates head position and pulls body segments forward
func (s *Snake) UpdatePosition(dt float64) {
	if !s.IsAlive {
		return
	}

	// Determine current speed based on boosting state
	if s.IsBoosting && s.Score > 0 {
		s.Speed = s.BoostSpeed
	} else {
		s.Speed = s.BaseSpeed
	}

	s.UpdateDirection(dt)

	// Move head forward
	moveDist := s.Speed * dt
	s.Head = physics.MovePoint(s.Head, s.Angle, moveDist)

	// Update body segments: each segment follows the one in front of it
	prev := s.Head
	for i := 0; i < len(s.Body); i++ {
		seg := s.Body[i]
		dist := physics.Distance(prev, seg)
		if dist > SegmentSpacing {
			angle := math.Atan2(prev.Y-seg.Y, prev.X-seg.X)
			s.Body[i] = physics.Vector2D{
				X: prev.X - math.Cos(angle)*SegmentSpacing,
				Y: prev.Y - math.Sin(angle)*SegmentSpacing,
			}
		}
		prev = s.Body[i]
	}
}

// Grow increases the snake length and score
func (s *Snake) Grow(amount int) {
	s.Score += amount
	s.TargetLength = InitialSegments + (s.Score / 5)

	for len(s.Body) < s.TargetLength {
		lastIdx := len(s.Body) - 1
		var newSeg physics.Vector2D
		if lastIdx >= 0 {
			newSeg = s.Body[lastIdx]
		} else {
			newSeg = s.Head
		}
		s.Body = append(s.Body, newSeg)
	}
}

// EatStaticFood records eating static food: 6 static foods = 1 score point & 1 body segment growth
func (s *Snake) EatStaticFood(count int) {
	if count <= 0 {
		count = 1
	}
	s.StaticFoodsEaten += count
	s.Score = s.StaticFoodsEaten / 6
	s.TargetLength = InitialSegments + (s.StaticFoodsEaten / 6)

	for len(s.Body) < s.TargetLength {
		lastIdx := len(s.Body) - 1
		var newSeg physics.Vector2D
		if lastIdx >= 0 {
			newSeg = s.Body[lastIdx]
		} else {
			newSeg = s.Head
		}
		s.Body = append(s.Body, newSeg)
	}
}

// SetDirectLocation updates the snake head location directly from client authoritative stream
func (s *Snake) SetDirectLocation(head physics.Vector2D, angle float64, isBoost bool) {
	if !s.IsAlive {
		return
	}
	s.Head = head
	s.Angle = angle
	s.TargetAngle = angle
	s.IsBoosting = isBoost

	prev := s.Head
	for i := 0; i < len(s.Body); i++ {
		seg := s.Body[i]
		dist := physics.Distance(prev, seg)
		if dist > SegmentSpacing {
			ang := math.Atan2(prev.Y-seg.Y, prev.X-seg.X)
			s.Body[i] = physics.Vector2D{
				X: prev.X - math.Cos(ang)*SegmentSpacing,
				Y: prev.Y - math.Sin(ang)*SegmentSpacing,
			}
		}
		prev = s.Body[i]
	}
}

