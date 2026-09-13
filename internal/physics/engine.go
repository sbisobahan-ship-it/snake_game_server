package physics

import (
	"math"
)

// Vector2D represents a 2D coordinate or velocity vector
type Vector2D struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Distance calculates Euclidean distance between two points
func Distance(p1, p2 Vector2D) float64 {
	dx := p1.X - p2.X
	dy := p1.Y - p2.Y
	return math.Sqrt(dx*dx + dy*dy)
}

// DistanceSquared calculates squared distance (faster for collision checks)
func DistanceSquared(p1, p2 Vector2D) float64 {
	dx := p1.X - p2.X
	dy := p1.Y - p2.Y
	return dx*dx + dy*dy
}

// NormalizeAngle keeps angle within [-pi, pi]
func NormalizeAngle(angle float64) float64 {
	for angle > math.Pi {
		angle -= 2 * math.Pi
	}
	for angle < -math.Pi {
		angle += 2 * math.Pi
	}
	return angle
}

// MovePoint moves a point in a given angle by distance
func MovePoint(origin Vector2D, angle float64, distance float64) Vector2D {
	return Vector2D{
		X: origin.X + math.Cos(angle)*distance,
		Y: origin.Y + math.Sin(angle)*distance,
	}
}

// Clamp restricts a value between min and max
func Clamp(val, min, max float64) float64 {
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}
