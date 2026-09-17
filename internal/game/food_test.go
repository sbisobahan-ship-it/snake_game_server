package game

import (
	"testing"

	"snake_game_server/internal/config"
)

func TestFoodEatAndImmediateRemoval(t *testing.T) {
	cfg := &config.Config{
		WorldWidth:  5000,
		WorldHeight: 5000,
		TickRate:    30,
	}

	var latestState *WorldState
	room := NewRoom("test-food-room", cfg, func(state *WorldState) {
		latestState = state
	})

	player := room.AddPlayer("test_eater", "Eater", 1)

	allFoods := room.GetAllFoodsDTO()
	if len(allFoods) == 0 {
		t.Fatalf("Expected pre-populated foods in room")
	}

	targetFood := allFoods[0]
	initialScore := player.Snake.Score

	// 1. Client 1 Eats Food
	colorIdx, gained, newScore, ok := room.EatFood("test_eater", targetFood.ID)
	if !ok {
		t.Fatalf("Expected EatFood to succeed")
	}

	if colorIdx != targetFood.ColorIndex {
		t.Errorf("Expected eaten colorIdx == %d, got %d", targetFood.ColorIndex, colorIdx)
	}

	if newScore <= initialScore || gained <= 0 {
		t.Errorf("Expected score increase, gained: %d, newScore: %d", gained, newScore)
	}

	// 2. Immediate 30 FPS tick broadcast instructs all clients to remove food
	room.Tick(0.033)

	if latestState == nil {
		t.Fatalf("Expected WorldState on tick")
	}

	if len(latestState.EatenFoods) == 0 {
		t.Errorf("Expected food to be marked in EatenFoods for all clients")
	} else if latestState.EatenFoods[0].FoodID != targetFood.ID {
		t.Errorf("Expected FoodID %d in frame, got %d", targetFood.ID, latestState.EatenFoods[0].FoodID)
	}

	// 3. Verify food is removed from server room map
	_, _, _, okSecond := room.EatFood("test_eater", targetFood.ID)
	if okSecond {
		t.Errorf("Expected food to be removed from room")
	}
}

func TestFoodExpirationAfterTTL(t *testing.T) {
	cfg := &config.Config{
		WorldWidth:  5000,
		WorldHeight: 5000,
		TickRate:    30,
	}

	room := NewRoom("test-ttl-room", cfg, func(state *WorldState) {})
	player := room.AddPlayer("eater_1", "Eater1", 1)

	foodID := uint32(99999)

	// Consume food at T = 1000
	now := int64(1000)
	room.mu.Lock()
	room.markFoodConsumedLocked(foodID, now)
	room.mu.Unlock()

	// At T = 5000 (after 4s): Still remembered / blocked
	room.mu.Lock()
	if !room.isFoodConsumedLocked(foodID, 5000) {
		t.Errorf("Expected food to be remembered within 8s TTL window")
	}
	room.mu.Unlock()

	// At T = 9001 (after 8001ms): Eviction should purge it
	room.mu.Lock()
	room.evictExpiredFoodsLocked(9001)
	if room.isFoodConsumedLocked(foodID, 9001) {
		t.Errorf("Expected food to be evicted and forgotten after 8s TTL window")
	}
	room.mu.Unlock()

	// Player can claim it again or new food can use the same ID without conflict
	accepted, _ := room.ClaimAndEatFood(player.ID, foodID, 1)
	if !accepted {
		t.Errorf("Expected ClaimAndEatFood to succeed after TTL expiration")
	}
}

func TestEnqueueEatAndFlushBatch(t *testing.T) {
	cfg := &config.Config{
		WorldWidth:  5000,
		WorldHeight: 5000,
		TickRate:    30,
	}

	room := NewRoom("test-batch-room", cfg, func(state *WorldState) {})
	p1 := room.AddPlayer("player_1", "Player1", 1)
	p2 := room.AddPlayer("player_2", "Player2", 2)

	foods := room.GetAllFoodsDTO()
	if len(foods) < 5 {
		t.Fatalf("Expected at least 5 foods")
	}

	var broadcastedBatches [][]FoodEatenEvent
	room.SetEatBatchCallback(func(events []FoodEatenEvent) {
		broadcastedBatches = append(broadcastedBatches, events)
	})

	// Multiple players eat multiple foods simultaneously via EnqueueEat
	room.EnqueueEat(p1.ID, foods[0].ID, 10)
	room.EnqueueEat(p2.ID, foods[1].ID, 15)
	room.EnqueueEat(p1.ID, foods[2].ID, 5)

	// Duplicate attempt: p2 tries to eat the same food as p1 (foods[0])
	room.EnqueueEat(p2.ID, foods[0].ID, 10)

	// Flush the batch atomically
	events := room.FlushEatBatch()

	if len(events) != 3 {
		t.Fatalf("Expected exactly 3 accepted events, got %d", len(events))
	}

	if len(broadcastedBatches) != 1 {
		t.Fatalf("Expected 1 batch broadcast, got %d", len(broadcastedBatches))
	}

	if len(broadcastedBatches[0]) != 3 {
		t.Errorf("Expected batch of 3 items, got %d", len(broadcastedBatches[0]))
	}

	if p1.Snake.Score != 15 { // 10 + 5
		t.Errorf("Expected p1 score 15, got %d", p1.Snake.Score)
	}

	if p2.Snake.Score != 15 { // 15
		t.Errorf("Expected p2 score 15, got %d", p2.Snake.Score)
	}
}

func TestDirectLocationStreamTriggersEat(t *testing.T) {
	cfg := &config.Config{
		WorldWidth:  5000,
		WorldHeight: 5000,
		TickRate:    30,
	}

	room := NewRoom("test-location-eat-room", cfg, func(state *WorldState) {})
	player := room.AddPlayer("stream_player", "Streamer", 1)

	allFoods := room.GetAllFoodsDTO()
	if len(allFoods) == 0 {
		t.Fatalf("Expected foods in room")
	}

	targetFood := allFoods[0]
	initialScore := player.Snake.Score

	var receivedEatenEvents []FoodEatenEvent
	room.SetEatBatchCallback(func(events []FoodEatenEvent) {
		receivedEatenEvents = append(receivedEatenEvents, events...)
	})

	// Client only streams snake location directly over the food coordinates
	eaten := room.UpdatePlayerLocation(player.ID, targetFood.X, targetFood.Y, 0.0, false)

	if len(eaten) == 0 {
		t.Fatalf("Expected server to detect collision and eat food")
	}

	if eaten[0].FoodID != targetFood.ID {
		t.Errorf("Expected FoodID %d, got %d", targetFood.ID, eaten[0].FoodID)
	}

	if player.Snake.Score <= initialScore {
		t.Errorf("Expected snake score to increase, got %d, initial %d", player.Snake.Score, initialScore)
	}

	// Verify real-time callback was invoked
	if len(receivedEatenEvents) == 0 {
		t.Fatalf("Expected real-time eat callback to be triggered")
	}

	if receivedEatenEvents[0].FoodID != targetFood.ID {
		t.Errorf("Expected callback FoodID %d, got %d", targetFood.ID, receivedEatenEvents[0].FoodID)
	}
}

func TestSpatialFoodGrid_IndexQueryAndRemove(t *testing.T) {
	grid := NewSpatialFoodGrid()

	// 1. Create sample foods scattered across the 30k x 30k arena
	foods := []*FoodItem{
		{ID: 1, X: 250, Y: 250, Value: 5, Radius: 10, ColorIndex: 1},     // Cell: (0, 0) -> idx: 0
		{ID: 2, X: 750, Y: 250, Value: 3, Radius: 8, ColorIndex: 2},      // Cell: (1, 0) -> idx: 1
		{ID: 3, X: 250, Y: 750, Value: 4, Radius: 9, ColorIndex: 3},      // Cell: (0, 1) -> idx: 60
		{ID: 4, X: 15000, Y: 15000, Value: 10, Radius: 12, ColorIndex: 4}, // Cell: (30, 30) -> idx: 1830
		{ID: 5, X: 29900, Y: 29900, Value: 2, Radius: 7, ColorIndex: 5},   // Cell: (59, 59) -> idx: 3599
	}

	// 2. Test IndexFoods
	grid.IndexFoods(foods)

	if grid.Count() != 5 {
		t.Fatalf("Expected 5 foods indexed, got %d", grid.Count())
	}

	// Verify cell index mapping formula: (floor(y/500)*60) + floor(x/500)
	if idx := GetCellIndex(250, 250); idx != 0 {
		t.Errorf("Expected cell 0 for (250, 250), got %d", idx)
	}
	if idx := GetCellIndex(750, 250); idx != 1 {
		t.Errorf("Expected cell 1 for (750, 250), got %d", idx)
	}
	if idx := GetCellIndex(250, 750); idx != 60 {
		t.Errorf("Expected cell 60 for (250, 750), got %d", idx)
	}
	if idx := GetCellIndex(15000, 15000); idx != 1830 {
		t.Errorf("Expected cell 1830 for (15000, 15000), got %d", idx)
	}
	if idx := GetCellIndex(29900, 29900); idx != 3599 {
		t.Errorf("Expected cell 3599 for (29900, 29900), got %d", idx)
	}

	// 3. Test QueryArea (Viewport AoI: [0, 0] to [1000, 1000])
	// Should retrieve Food #1, #2, #3, but NOT Food #4 or #5
	viewportFoods := grid.QueryArea(0, 0, 1000, 1000)
	if len(viewportFoods) != 3 {
		t.Fatalf("Expected 3 foods in viewport [0, 0, 1000, 1000], got %d", len(viewportFoods))
	}

	foundIDs := make(map[uint32]bool)
	for _, f := range viewportFoods {
		foundIDs[f.ID] = true
	}
	if !foundIDs[1] || !foundIDs[2] || !foundIDs[3] {
		t.Errorf("Expected Food IDs 1, 2, 3 in viewport, got %+v", foundIDs)
	}
	if foundIDs[4] || foundIDs[5] {
		t.Errorf("Did not expect distant foods in viewport query")
	}

	// Query middle arena: [14000, 14000] to [16000, 16000]
	midFoods := grid.QueryArea(14000, 14000, 16000, 16000)
	if len(midFoods) != 1 || midFoods[0].ID != 4 {
		t.Errorf("Expected only Food #4 in mid query, got %d items", len(midFoods))
	}

	// 4. Test RemoveFood (O(1) instant removal upon eating)
	removed := grid.RemoveFood(2)
	if removed == nil || removed.ID != 2 {
		t.Fatalf("Expected Food #2 to be removed and returned")
	}

	if grid.Count() != 4 {
		t.Errorf("Expected count 4 after removal, got %d", grid.Count())
	}

	if grid.GetFood(2) != nil {
		t.Errorf("Expected Food #2 to no longer exist in grid")
	}

	// Re-query viewport: Food #2 should no longer be present
	viewportAfterRemove := grid.QueryArea(0, 0, 1000, 1000)
	if len(viewportAfterRemove) != 2 {
		t.Fatalf("Expected 2 foods in viewport after removal, got %d", len(viewportAfterRemove))
	}

	// Remove non-existent food
	if grid.RemoveFood(99999) != nil {
		t.Errorf("Expected nil when removing non-existent food")
	}
}


