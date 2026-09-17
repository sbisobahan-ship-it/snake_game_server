package game

import (
	"math"
	"sync/atomic"
	"time"

	"snake_game_server/internal/monitor"
)

// BotEatAreaResult contains results of an interactive bot eat action
type BotEatAreaResult struct {
	Status      string           `json:"status"`
	BotID       string           `json:"bot_id"`
	CenterX     float64          `json:"center_x"`
	CenterY     float64          `json:"center_y"`
	Radius      float64          `json:"radius"`
	FoodsEaten  int              `json:"foods_eaten"`
	ScoreGained int              `json:"score_gained"`
	Timestamp   int64            `json:"timestamp"`
}

// LivePlayerSummary contains lightweight position and metadata for live visualizer
type LivePlayerSummary struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	HeadX    float64 `json:"x"`
	HeadY    float64 `json:"y"`
	Angle    float64 `json:"angle"`
	Score    int     `json:"score"`
	IsAlive  bool    `json:"alive"`
	SkinID   int     `json:"skin"`
	Segments int     `json:"segments"`
}

// GetLivePlayersSummary returns simple positions and scores of all living players for the monitor/visualizer
func (r *Room) GetLivePlayersSummary() []LivePlayerSummary {
	r.mu.RLock()
	defer r.mu.RUnlock()

	res := make([]LivePlayerSummary, 0, len(r.Players))
	for _, p := range r.Players {
		if p.Snake != nil {
			res = append(res, LivePlayerSummary{
				ID:       p.ID,
				Name:     p.Name,
				HeadX:    p.Snake.Head.X,
				HeadY:    p.Snake.Head.Y,
				Angle:    p.Snake.Angle,
				Score:    p.Snake.Score,
				IsAlive:  p.Snake.IsAlive,
				SkinID:   p.Snake.SkinID,
				Segments: len(p.Snake.Body),
			})
		}
	}
	return res
}

// BotEatArea simulates an interactive test bot eating all static and dynamic foods within a circular area
func (r *Room) BotEatArea(centerX, centerY, radius float64, botID string) BotEatAreaResult {
	if botID == "" {
		botID = "bot_tester"
	}
	if radius <= 0 {
		radius = 500.0
	}

	r.mu.Lock()

	now := time.Now().UnixMilli()
	var eatenEvents []FoodEatenEvent
	var scoreGained int
	radSq := radius * radius

	// 1. Check Static Foods in the requested area
	if r.StaticFoods != nil {
		r.StaticFoods.mu.Lock()
		minGX := int(math.Max(0, math.Min(99, math.Floor((centerX-radius)/r.StaticFoods.gridSize))))
		maxGX := int(math.Max(0, math.Min(99, math.Floor((centerX+radius)/r.StaticFoods.gridSize))))
		minGY := int(math.Max(0, math.Min(99, math.Floor((centerY-radius)/r.StaticFoods.gridSize))))
		maxGY := int(math.Max(0, math.Min(99, math.Floor((centerY+radius)/r.StaticFoods.gridSize))))

		for gx := minGX; gx <= maxGX; gx++ {
			for gy := minGY; gy <= maxGY; gy++ {
				for _, food := range r.StaticFoods.grid[gx][gy] {
					if !food.IsActive {
						continue
					}
					dx := centerX - float64(food.X)
					dy := centerY - float64(food.Y)
					if dx*dx+dy*dy <= radSq {
						food.IsActive = false
						food.RespawnAt = now + r.StaticFoods.respawnDelayMs
						scoreGained += int(food.Value)

						eatenEvents = append(eatenEvents, FoodEatenEvent{
							FoodID:     food.ID,
							ColorIndex: food.FruitIndex,
							EaterID:    botID,
							NewScore:   scoreGained,
						})
					}
				}
			}
		}
		r.StaticFoods.mu.Unlock()
	}

	// 2. Check Dynamic Foods in the requested area
	for id, f := range r.Foods {
		if f == nil {
			continue
		}
		dx := centerX - f.Pos.X
		dy := centerY - f.Pos.Y
		if dx*dx+dy*dy <= radSq {
			scoreGained += f.Value
			eatenEvents = append(eatenEvents, FoodEatenEvent{
				FoodID:     f.ID,
				ColorIndex: f.ColorIndex,
				EaterID:    botID,
				NewScore:   scoreGained,
			})
			delete(r.Foods, id)
		}
	}

	var eatFn func(events []FoodEatenEvent)
	if len(eatenEvents) > 0 {
		atomic.AddUint64(&r.totalEaten, uint64(len(eatenEvents)))
		r.pendingEaten = append(r.pendingEaten, eatenEvents...)
		eatFn = r.onEatBatchFn
	}

	r.mu.Unlock()

	if len(eatenEvents) > 0 {
		if eatFn != nil {
			eventsCopy := make([]FoodEatenEvent, len(eatenEvents))
			copy(eventsCopy, eatenEvents)
			eatFn(eventsCopy)
		}
		monitor.DefaultHub.Emit(monitor.ChanFood, "eat", "🤖 [BOT TESTER EAT] Bot '%s' consumed %d food(s) at (%.0f, %.0f | R: %.0f) -> Gained %d pts", botID, len(eatenEvents), centerX, centerY, radius, scoreGained)
	}

	return BotEatAreaResult{
		Status:      "ok",
		BotID:       botID,
		CenterX:     centerX,
		CenterY:     centerY,
		Radius:      radius,
		FoodsEaten:  len(eatenEvents),
		ScoreGained: scoreGained,
		Timestamp:   now,
	}
}
