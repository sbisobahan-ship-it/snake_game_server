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

	// Test boundary clamping by forcing position outside bounds [0, 1000]
	p1.Snake.Head = physics.Vector2D{X: 1200, Y: -50}
	room.Tick(0.033)

	if p1.Snake.Head.X > 1000 {
		t.Errorf("Expected snake head to be clamped to <= 1000, got %f", p1.Snake.Head.X)
	}
	if p1.Snake.Head.Y < 0 {
		t.Errorf("Expected snake head to be clamped to >= 0, got %f", p1.Snake.Head.Y)
	}
	if !p1.Snake.IsAlive {
		t.Errorf("Expected snake to remain alive with boundary clamping")
	}
}

func TestServerAuthoritativeDeadReckoningDuringNetworkLag(t *testing.T) {
	cfg := &config.Config{
		WorldWidth:  5000,
		WorldHeight: 5000,
		TickRate:    30,
	}
	room := NewRoom("test-dead-reckoning", cfg, nil)

	p := room.AddPlayer("laggy_player", "LaggyPlayer", 1)
	p.Snake.Head = physics.Vector2D{X: 1000, Y: 1000}
	p.Snake.Angle = 0.0 // Moving East along positive X axis
	p.Snake.TargetAngle = 0.0
	p.Snake.Speed = 200.0

	// Spawn food at (1050, 1000) directly in snake's path
	foodID := uint32(9999)
	room.Foods[foodID] = NewFood(foodID, physics.Vector2D{X: 1050, Y: 1000}, 5, 12.0, 1, false)

	// Simulate 1.0 second of network disconnect / no client packets (30 ticks of 0.033s)
	for i := 0; i < 30; i++ {
		room.Tick(0.0333)
	}

	// 1. Verify snake continued moving forward authoritatively along its angle
	syncDTO, ok := room.GetPlayerSync(p.ID)
	if !ok {
		t.Fatalf("Expected player sync DTO to exist")
	}

	if syncDTO.Head.X <= 1050 {
		t.Errorf("Expected snake to have moved forward past 1050, got %f", syncDTO.Head.X)
	}

	// 2. Verify server authoritatively ate the food along its path during the lag
	if syncDTO.Score < 5 {
		t.Errorf("Expected score to increase to >= 5 from eating food during lag, got %d", syncDTO.Score)
	}

	// 3. Verify angle and alive state preserved
	if syncDTO.Angle != 0.0 {
		t.Errorf("Expected angle 0.0 preserved, got %f", syncDTO.Angle)
	}
	if !syncDTO.IsAlive {
		t.Errorf("Expected snake to remain alive")
	}
}

func TestDisconnectPreservesPlayerAndReconnectionSync(t *testing.T) {
	cfg := &config.Config{
		WorldWidth:  5000,
		WorldHeight: 5000,
		TickRate:    30,
	}
	room := NewRoom("test-reconnect-room", cfg, nil)

	// 1. Initial join
	playerID := "player_reconnect_test"
	p1 := room.AddPlayer(playerID, "Hero", 1)
	p1.Snake.Head = physics.Vector2D{X: 2000, Y: 2000}
	p1.Snake.Angle = 0.0 // Moving East
	p1.Snake.TargetAngle = 0.0
	p1.Snake.Speed = 200.0

	// 2. Client loses connection (Socket drops)
	room.MarkPlayerDisconnected(playerID)

	// Verify player is marked disconnected but still present in room
	if p1.IsConnected {
		t.Errorf("Expected IsConnected to be false after disconnect")
	}
	if room.GetPlayerCount() != 1 {
		t.Errorf("Expected player to remain in room, count is %d", room.GetPlayerCount())
	}

	// 3. Server continues simulation while player is offline
	for i := 0; i < 20; i++ {
		room.Tick(0.0333)
	}

	// 4. Client reconnects with same player ID
	pReconnected := room.AddPlayer(playerID, "Hero", 1)

	// Verify same player entity is preserved, NOT re-spawned at a random position
	if pReconnected != p1 {
		t.Errorf("Expected reconnected player to match original instance")
	}
	if !pReconnected.IsConnected {
		t.Errorf("Expected IsConnected to be true after reconnect")
	}
	if pReconnected.Snake.Head.X <= 2000 {
		t.Errorf("Expected snake to have progressed forward past 2000, got %f", pReconnected.Snake.Head.X)
	}
	if !pReconnected.Snake.IsAlive {
		t.Errorf("Expected snake to remain alive on reconnect")
	}
}

func TestStaticFoodScoringAndGrowth(t *testing.T) {
	snake := NewSnake("p1", physics.Vector2D{X: 100, Y: 100}, 0, 1)

	// Initially 0 foods, 0 score, InitialSegments
	if snake.Score != 0 || snake.StaticFoodsEaten != 0 || len(snake.Body) != InitialSegments {
		t.Fatalf("Initial state mismatch: score=%d, foods=%d, body=%d", snake.Score, snake.StaticFoodsEaten, len(snake.Body))
	}

	// Eat 5 static foods -> Score should still be 0, segments = InitialSegments
	for i := 0; i < 5; i++ {
		snake.EatStaticFood(1)
	}
	if snake.Score != 0 {
		t.Errorf("Expected score 0 after 5 foods, got %d", snake.Score)
	}
	if snake.StaticFoodsEaten != 5 {
		t.Errorf("Expected 5 static foods eaten, got %d", snake.StaticFoodsEaten)
	}
	if len(snake.Body) != InitialSegments {
		t.Errorf("Expected %d segments after 5 foods, got %d", InitialSegments, len(snake.Body))
	}

	// Eat 6th static food -> Score should become 1, segments = InitialSegments + 1
	snake.EatStaticFood(1)
	if snake.Score != 1 {
		t.Errorf("Expected score 1 after 6 foods, got %d", snake.Score)
	}
	if snake.StaticFoodsEaten != 6 {
		t.Errorf("Expected 6 static foods eaten, got %d", snake.StaticFoodsEaten)
	}
	if len(snake.Body) != InitialSegments+1 {
		t.Errorf("Expected %d segments after 6 foods, got %d", InitialSegments+1, len(snake.Body))
	}

	// Eat 6 more static foods (total 12) -> Score should become 2, segments = InitialSegments + 2
	for i := 0; i < 6; i++ {
		snake.EatStaticFood(1)
	}
	if snake.Score != 2 {
		t.Errorf("Expected score 2 after 12 foods, got %d", snake.Score)
	}
	if snake.StaticFoodsEaten != 12 {
		t.Errorf("Expected 12 static foods eaten, got %d", snake.StaticFoodsEaten)
	}
	if len(snake.Body) != InitialSegments+2 {
		t.Errorf("Expected %d segments after 12 foods, got %d", InitialSegments+2, len(snake.Body))
	}
}



