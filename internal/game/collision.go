package game

import (
	"snake_game_server/internal/physics"
)

// CheckBoundaryCollision checks if snake head touches or crosses the arena border margin (e.g. 220.0 brick thickness).
// Playable area: (X: borderMargin to worldWidth - borderMargin, Y: borderMargin to worldHeight - borderMargin)
func CheckBoundaryCollision(head physics.Vector2D, radius float64, worldWidth, worldHeight float64, borderMargin float64) bool {
	if head.X-radius <= borderMargin || head.X+radius >= (worldWidth-borderMargin) ||
		head.Y-radius <= borderMargin || head.Y+radius >= (worldHeight-borderMargin) {
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
	if target == nil || !target.IsAlive {
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

// CheckSnakeHeadCollision checks if two snake heads collide
func CheckSnakeHeadCollision(head1 physics.Vector2D, r1 float64, head2 physics.Vector2D, r2 float64) bool {
	combinedRadius := r1 + r2
	return physics.DistanceSquared(head1, head2) <= (combinedRadius * combinedRadius)
}
