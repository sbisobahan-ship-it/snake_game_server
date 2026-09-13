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
