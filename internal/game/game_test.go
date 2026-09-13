package game

import (
	"testing"

	"snake_game_server/internal/config"
	"snake_game_server/internal/physics"
)

func TestSnakeMovementAndGrowth(t *testing.T) {
	spawnPos := physics.Vector2D{X: 0, Y: 0}
	snake := NewSnake("test-player", spawnPos, 0, 1)

	if !snake.IsAlive {
		t.Fatalf("Expected snake to be alive on spawn")
	}

	// 1. Test movement
	dt := 0.1 // 100ms
	snake.UpdatePosition(dt)

	expectedX := spawnPos.X + snake.Speed*dt
	if snake.Head.X <= spawnPos.X {
		t.Errorf("Expected head X to move forward, got %f, want > %f", snake.Head.X, expectedX)
	}

	// 2. Test Growth
	initialLength := len(snake.Body)
	snake.Grow(5)
	if snake.Score != 5 {
		t.Errorf("Expected score 5, got %d", snake.Score)
	}
	if len(snake.Body) <= initialLength {
		t.Errorf("Expected body length to increase, got %d", len(snake.Body))
	}
}

func TestCollisionDetection(t *testing.T) {
	cfg := &config.Config{
		WorldWidth:  1000,
		WorldHeight: 1000,
		TickRate:    30,
	}
	room := NewRoom("test-room", cfg, nil)

	p1 := room.AddPlayer("p1", "Player One", 1)
	if p1 == nil || !p1.Snake.IsAlive {
		t.Fatalf("Player 1 failed to spawn")
	}

	// Test boundary clamping by forcing position outside bounds
	p1.Snake.Head = physics.Vector2D{X: 600, Y: 0} // Outside 1000x1000 (halfW = 500)
	room.Tick(0.033)

	if p1.Snake.Head.X > 500 {
		t.Errorf("Expected snake head to be clamped to <= 500, got %f", p1.Snake.Head.X)
	}
	if !p1.Snake.IsAlive {
		t.Errorf("Expected snake to remain alive with boundary clamping")
	}
}
