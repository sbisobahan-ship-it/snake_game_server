package game

import (
	"testing"
	"time"
)

func TestStaticFoodRegistry_LoadAndCollision(t *testing.T) {
	// Initialize without CSV path to test 100% deterministic generator
	reg, err := NewStaticFoodRegistry("", 300.0)
	if err != nil {
		t.Fatalf("Failed to load static foods: %v", err)
	}

	if reg.GetTotalLoaded() != 10000 {
		t.Errorf("Expected exactly 10000 foods, got %d", reg.GetTotalLoaded())
	}

	activeCount := reg.CountActive()
	if activeCount != 10000 {
		t.Errorf("Expected 10000 active foods initially, got %d", activeCount)
	}

	// Food #1 is at (200, 200)
	f1, exists := reg.foodByID[1]
	if !exists {
		t.Fatalf("Food #1 not found in registry")
	}
	if f1.X != 200.0 || f1.Y != 200.0 {
		t.Errorf("Expected Food #1 to be at (200, 200), got (%.1f, %.1f)", f1.X, f1.Y)
	}

	// Test collision with snake head at (205, 205) with head radius 14.0
	events := reg.CheckCollisions("test_player", 205.0, 205.0, 14.0)
	if len(events) == 0 {
		t.Fatalf("Expected collision with Food #1, got 0 events")
	}

	found := false
	for _, e := range events {
		if e.FoodID == 1 {
			found = true
			if e.EaterID != "test_player" {
				t.Errorf("Expected EaterID test_player, got %s", e.EaterID)
			}
		}
	}
	if !found {
		t.Errorf("Expected FoodID 1 in eaten events, got %+v", events)
	}

	// Second collision at same spot should yield 0 events since food is already eaten
	events2 := reg.CheckCollisions("test_player", 205.0, 205.0, 14.0)
	if len(events2) > 0 {
		t.Errorf("Expected 0 events after food was consumed, got %d", len(events2))
	}

	// Test respawn after 10s timer expires
	reg.mu.Lock()
	reg.foods[0].RespawnAt = time.Now().UnixMilli() - 100 // Force expiration
	reg.mu.Unlock()

	respawned := reg.TickRespawns(time.Now().UnixMilli())
	if respawned < 1 {
		t.Errorf("Expected at least 1 food to respawn, got %d", respawned)
	}

	// Now food #1 should be active again
	events3 := reg.CheckCollisions("test_player", 205.0, 205.0, 14.0)
	if len(events3) == 0 {
		t.Errorf("Expected food #1 to be edible again after respawn")
	}
}
