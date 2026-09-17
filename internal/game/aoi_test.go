package game

import (
	"testing"

	"snake_game_server/internal/config"
	"snake_game_server/internal/physics"
)

func TestRoom_AoISpatialFiltering(t *testing.T) {
	cfg := &config.Config{
		WorldWidth:   30000.0,
		WorldHeight:  30000.0,
		GridCellSize: 300.0,
		AoIRadius:    2400.0,
		TickRate:     30,
	}

	room := NewRoom("aoi-test-room", cfg, func(state *WorldState) {})

	// 1. Add Player 1 at (2000, 2000)
	p1 := room.AddPlayer("p1", "PlayerOne", 1)
	p1.Snake.Head = physics.Vector2D{X: 2000.0, Y: 2000.0}

	// 2. Add Player 2 (Nearby) at (2500, 2500) -> distance ~ 707 units (within 2400 radius)
	p2 := room.AddPlayer("p2", "PlayerTwo", 2)
	p2.Snake.Head = physics.Vector2D{X: 2500.0, Y: 2500.0}

	// 3. Add Player 3 (Far Away) at (25000, 25000) -> distance > 30000 units (far outside 2400 radius)
	p3 := room.AddPlayer("p3", "PlayerThree", 3)
	p3.Snake.Head = physics.Vector2D{X: 25000.0, Y: 25000.0}

	// 4. Add dynamic spawned foods: one near (2100, 2100), one far at (26000, 26000)
	room.mu.Lock()
	room.pendingSpawned = []FoodSpawnEvent{
		{FoodID: 90001, ColorIndex: 1, X: 2100.0, Y: 2100.0, Value: 10},
		{FoodID: 90002, ColorIndex: 2, X: 26000.0, Y: 26000.0, Value: 10},
	}
	// Also register in dynamic Foods so position lookup works
	fNear := NewFood(90001, physics.Vector2D{X: 2100.0, Y: 2100.0}, 10, 12.0, 1, false)
	fFar := NewFood(90002, physics.Vector2D{X: 26000.0, Y: 26000.0}, 10, 12.0, 2, false)
	room.Foods[90001] = fNear
	room.Foods[90002] = fFar

	// Add pending eaten food events
	room.pendingEaten = []FoodEatenEvent{
		{FoodID: 90001, EaterID: "p2", NewScore: 20}, // Eaten near p1 by p2 -> should be visible to p1
		{FoodID: 90002, EaterID: "p3", NewScore: 50}, // Eaten far by p3 -> should NOT be visible to p1
		{FoodID: 99999, EaterID: "p1", NewScore: 10}, // Eaten by p1 -> ALWAYS visible to p1
	}
	room.mu.Unlock()

	// --- TEST: GetWorldStateForPlayer for P1 ---
	stateP1 := room.GetWorldStateForPlayer("p1", 2400.0)
	if stateP1 == nil {
		t.Fatalf("Expected non-nil WorldState for P1")
	}

	// Verify Player Filtering: P1 and P2 should be present, P3 should be filtered out
	playerMap := make(map[string]bool)
	for _, p := range stateP1.Players {
		playerMap[p.ID] = true
	}

	if !playerMap["p1"] {
		t.Errorf("P1 must be present in its own AoI snapshot")
	}
	if !playerMap["p2"] {
		t.Errorf("P2 (nearby) should be present in P1's AoI snapshot")
	}
	if playerMap["p3"] {
		t.Errorf("P3 (far away at 25k, 25k) must NOT be present in P1's AoI snapshot")
	}

	// Verify Spawned Foods Filtering: 90001 present, 90002 filtered out
	spawnedMap := make(map[uint32]bool)
	for _, sf := range stateP1.SpawnedFoods {
		spawnedMap[sf.FoodID] = true
	}
	if !spawnedMap[90001] {
		t.Errorf("Nearby spawned food 90001 should be present")
	}
	if spawnedMap[90002] {
		t.Errorf("Far away spawned food 90002 must NOT be present")
	}

	// Verify Eaten Foods Filtering: 90001 (nearby eaten by p2) & 99999 (p1's own eat) present, 90002 excluded
	eatenMap := make(map[uint32]bool)
	for _, ef := range stateP1.EatenFoods {
		eatenMap[ef.FoodID] = true
	}
	if !eatenMap[90001] {
		t.Errorf("Nearby eaten food 90001 should be present in P1's snapshot")
	}
	if !eatenMap[99999] {
		t.Errorf("P1's own eaten food 99999 must ALWAYS be present in P1's snapshot")
	}
	if eatenMap[90002] {
		t.Errorf("Distant eaten food 90002 by stranger must NOT be present in P1's snapshot")
	}

	// --- TEST: FilterEatEventsForPlayer ---
	eatEvents := []FoodEatenEvent{
		{FoodID: 90001, EaterID: "p2", NewScore: 20},
		{FoodID: 90002, EaterID: "p3", NewScore: 50},
		{FoodID: 99999, EaterID: "p1", NewScore: 10},
	}
	filteredForP1 := room.FilterEatEventsForPlayer("p1", eatEvents, 2400.0)
	if len(filteredForP1) != 2 {
		t.Fatalf("Expected 2 events for P1, got %d", len(filteredForP1))
	}

	filteredForP3 := room.FilterEatEventsForPlayer("p3", eatEvents, 2400.0)
	if len(filteredForP3) != 1 || filteredForP3[0].FoodID != 90002 {
		t.Fatalf("Expected only event 90002 for P3, got %+v", filteredForP3)
	}
}

func TestRoom_BotEatArea(t *testing.T) {
	cfg := &config.Config{
		WorldWidth:   30000.0,
		WorldHeight:  30000.0,
		GridCellSize: 300.0,
		AoIRadius:    2400.0,
		TickRate:     30,
	}

	room := NewRoom("bot-eat-test-room", cfg, func(state *WorldState) {})

	// Add dynamic food at (5000, 5000)
	room.mu.Lock()
	room.Foods[88888] = NewFood(88888, physics.Vector2D{X: 5000.0, Y: 5000.0}, 25, 12.0, 1, false)
	room.mu.Unlock()

	// Simulate Bot eating with radius 600 around (5000, 5000)
	res := room.BotEatArea(5000.0, 5000.0, 600.0, "test_bot_01")
	if res.Status != "ok" {
		t.Fatalf("Expected status ok, got %s", res.Status)
	}
	if res.BotID != "test_bot_01" {
		t.Errorf("Expected BotID test_bot_01, got %s", res.BotID)
	}
	if res.FoodsEaten < 1 {
		t.Errorf("Expected at least 1 food eaten by bot, got %d", res.FoodsEaten)
	}
	if res.ScoreGained < 25 {
		t.Errorf("Expected score gain >= 25, got %d", res.ScoreGained)
	}

	// Verify live players summary
	playersSummary := room.GetLivePlayersSummary()
	if playersSummary == nil {
		t.Errorf("Expected non-nil live players summary")
	}
}

