package game

import (
	"snake_game_server/internal/physics"
)

// CheckBoundaryCollision checks if snake head has breached the arena boundary
func CheckBoundaryCollision(head physics.Vector2D, radius float64, worldWidth, worldHeight float64) bool {
	halfW := worldWidth / 2.0
	halfH := worldHeight / 2.0

	// Arena centered at (0,0) or (halfW, halfH). Using (0,0) centered bounds [-halfW, halfW]
	if head.X-radius < -halfW || head.X+radius > halfW ||
		head.Y-radius < -halfH || head.Y+radius > halfH {
		return true
	}
	return false
}

// CheckFoodCollision returns true if snake head overlaps with a food item
func CheckFoodCollision(head physics.Vector2D, headRadius float64, food *Food) bool {
	combinedRadius := headRadius + food.Radius
	return physics.DistanceSquared(head, food.Pos) <= (combinedRadius * combinedRadius)
}

// CheckSnakeBodyCollision checks if the attacking snake's head hits any body segment of target snake
func CheckSnakeBodyCollision(head physics.Vector2D, headRadius float64, target *Snake) bool {
	if !target.IsAlive {
		return false
	}

	collisionDistSq := (headRadius + target.BodyRadius) * (headRadius + target.BodyRadius)

	// Check against all body segments of target
	for _, seg := range target.Body {
		if physics.DistanceSquared(head, seg) <= collisionDistSq {
			return true
		}
	}
	return false
}
