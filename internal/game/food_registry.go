package game

import (
	"bufio"
	"encoding/csv"
	"io"
	"math"
	"os"
	"strconv"
	"sync"
	"time"

	"snake_game_server/internal/monitor"
)

// StaticFood represents one immutable food entity on the 30kx30k canvas
type StaticFood struct {
	ID         uint32  `json:"id"`
	GX         int     `json:"gx"`
	GY         int     `json:"gy"`
	X          float32 `json:"x"`
	Y          float32 `json:"y"`
	Value      uint16  `json:"val"`
	Size       float32 `json:"size"`
	Radius     float32 `json:"r"`
	FruitIndex uint8   `json:"fruit_index"`
	IsActive   bool    `json:"active"`
	RespawnAt  int64   `json:"respawn_at"`
}

// StaticFoodRegistry manages the authoritative 10k static food spatial dataset
type StaticFoodRegistry struct {
	mu             sync.RWMutex
	foods          []StaticFood           // All 10,000 foods
	foodByID       map[uint32]*StaticFood // Direct ID lookup
	grid           [100][100][]*StaticFood // 100x100 spatial grid for O(1) cell queries
	gridSize       float64                // Cell size in world coordinates (default 300.0)
	respawnDelayMs int64                  // Respawn cooldown (default 10000 ms = 10s)
	totalLoaded    int
}

// GenerateStaticFoods creates 10,000 location-locked deterministic static foods matching the Android client 100%
func GenerateStaticFoods(cols, rows int, worldW, worldH, spawnMargin float64) []StaticFood {
	count := cols * rows
	foods := make([]StaticFood, 0, count)
	cellW := worldW / float64(cols)
	cellH := worldH / float64(rows)

	for gy := 0; gy < rows; gy++ {
		for gx := 0; gx < cols; gx++ {
			id := uint32(gy*cols + gx + 1)
			seed := ((int64(gx) * 73856093) ^ (int64(gy) * 19349663) ^ 83492791) & 0x7FFFFFFF
			offsetX := 40.0 + float64(seed%22000)/100.0
			offsetY := 40.0 + float64((seed/220)%22000)/100.0

			rawX := float64(gx)*cellW + offsetX
			rawY := float64(gy)*cellH + offsetY

			// Round to 1 decimal place for absolute numerical stability
			x := float64(int(rawX*10.0)) / 10.0
			if x < spawnMargin {
				x = spawnMargin
			} else if x > worldW-spawnMargin {
				x = worldW - spawnMargin
			}

			y := float64(int(rawY*10.0)) / 10.0
			if y < spawnMargin {
				y = spawnMargin
			} else if y > worldH-spawnMargin {
				y = worldH - spawnMargin
			}

			fruitIndex := uint8(seed % 24)
			val := uint16(5 + ((seed / 31) % 6))
			size := float32(62.0 + float64(val-5)*5.0)
			radius := size / 2.0

			foods = append(foods, StaticFood{
				ID:         id,
				GX:         gx,
				GY:         gy,
				X:          float32(x),
				Y:          float32(y),
				Value:      val,
				Size:       size,
				Radius:     radius,
				FruitIndex: fruitIndex,
				IsActive:   true,
				RespawnAt:  0,
			})
		}
	}
	return foods
}

// NewStaticFoodRegistry initializes and loads the 10k static foods dataset
func NewStaticFoodRegistry(csvPath string, gridSize float64) (*StaticFoodRegistry, error) {
	if gridSize <= 0 {
		gridSize = 300.0
	}

	reg := &StaticFoodRegistry{
		foods:          make([]StaticFood, 0, 10500),
		foodByID:       make(map[uint32]*StaticFood, 10500),
		gridSize:       gridSize,
		respawnDelayMs: 10000, // 10 seconds respawn cooldown (server-based)
	}

	var err error
	if csvPath != "" {
		err = reg.loadCSV(csvPath)
	}
	if err != nil || reg.totalLoaded == 0 {
		reg.loadDeterministic(100, 100, 30000.0, 30000.0, 200.0)
	}

	monitor.DefaultHub.Emit(monitor.ChanFood, "info", "🍎 [STATIC FOOD REGISTRY] Loaded %d static foods (100%% Deterministic) into 100x100 spatial grid (Canvas: 30000x30000, Respawn: 10s)", reg.totalLoaded)
	return reg, nil
}

func (r *StaticFoodRegistry) loadDeterministic(cols, rows int, worldW, worldH, spawnMargin float64) {
	r.foods = GenerateStaticFoods(cols, rows, worldW, worldH, spawnMargin)
	for i := range r.foods {
		ptr := &r.foods[i]
		r.foodByID[ptr.ID] = ptr

		gx := ptr.GX
		gy := ptr.GY
		if gx >= 0 && gx < 100 && gy >= 0 && gy < 100 {
			r.grid[gx][gy] = append(r.grid[gx][gy], ptr)
		}
	}
	r.totalLoaded = len(r.foods)
}

func (r *StaticFoodRegistry) loadCSV(csvPath string) error {
	file, err := os.Open(csvPath)
	if err != nil {
		return err
	}
	defer file.Close()

	reader := csv.NewReader(bufio.NewReader(file))
	reader.FieldsPerRecord = -1

	// Read header
	header, err := reader.Read()
	if err != nil {
		return err
	}
	_ = header

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		if len(record) < 8 {
			continue
		}

		id64, err := strconv.ParseUint(record[0], 10, 32)
		if err != nil {
			continue
		}
		gx, _ := strconv.Atoi(record[1])
		gy, _ := strconv.Atoi(record[2])
		x, _ := strconv.ParseFloat(record[3], 32)
		y, _ := strconv.ParseFloat(record[4], 32)
		val, _ := strconv.ParseUint(record[5], 10, 16)
		size, _ := strconv.ParseFloat(record[6], 32)
		fruitIdx, _ := strconv.ParseUint(record[7], 10, 8)

		radius := float32(size) / 2.0
		if radius <= 0 {
			radius = 12.0
		}
		if val <= 0 {
			val = 1
		}

		food := StaticFood{
			ID:         uint32(id64),
			GX:         gx,
			GY:         gy,
			X:          float32(x),
			Y:          float32(y),
			Value:      uint16(val),
			Size:       float32(size),
			Radius:     radius,
			FruitIndex: uint8(fruitIdx),
			IsActive:   true,
			RespawnAt:  0,
		}

		r.foods = append(r.foods, food)
	}

	// Index into fast lookups & spatial grid
	for i := range r.foods {
		ptr := &r.foods[i]
		r.foodByID[ptr.ID] = ptr

		gx := ptr.GX
		gy := ptr.GY
		if gx >= 0 && gx < 100 && gy >= 0 && gy < 100 {
			r.grid[gx][gy] = append(r.grid[gx][gy], ptr)
		}
	}

	r.totalLoaded = len(r.foods)
	return nil
}

// CheckCollisions performs real-time collision detection for a snake head on the 30kx30k canvas
func (r *StaticFoodRegistry) CheckCollisions(playerID string, headX, headY, headRadius float64) []FoodEatenEvent {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now().UnixMilli()
	var eaten []FoodEatenEvent

	gx := int(headX / r.gridSize)
	gy := int(headY / r.gridSize)

	minGX := int(math.Max(0, math.Min(99, float64(gx-1))))
	maxGX := int(math.Max(0, math.Min(99, float64(gx+1))))
	minGY := int(math.Max(0, math.Min(99, float64(gy-1))))
	maxGY := int(math.Max(0, math.Min(99, float64(gy+1))))

	if minGX > maxGX || minGY > maxGY {
		return nil
	}

	// Base eating mouth radius for smooth eating detection
	effectiveHeadRadius := headRadius
	if effectiveHeadRadius < 42.0 {
		effectiveHeadRadius = 42.0
	}

	for xCell := minGX; xCell <= maxGX; xCell++ {
		for yCell := minGY; yCell <= maxGY; yCell++ {
			cellFoods := r.grid[xCell][yCell]
			for _, food := range cellFoods {
				if !food.IsActive {
					continue
				}

				dx := headX - float64(food.X)
				dy := headY - float64(food.Y)
				distSq := dx*dx + dy*dy

				hitRadius := effectiveHeadRadius + float64(food.Radius)
				if distSq <= hitRadius*hitRadius {
					// Food consumed!
					food.IsActive = false
					food.RespawnAt = now + r.respawnDelayMs

					eaten = append(eaten, FoodEatenEvent{
						FoodID:     food.ID,
						ColorIndex: food.FruitIndex,
						EaterID:    playerID,
						NewScore:   int(food.Value),
					})
				}
			}
		}
	}

	return eaten
}

// ClaimFood manually claims a food item by ID (used for direct client eat requests)
func (r *StaticFoodRegistry) ClaimFood(foodID uint32, playerID string) (*StaticFood, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	food, exists := r.foodByID[foodID]
	if !exists || !food.IsActive {
		return nil, false
	}

	now := time.Now().UnixMilli()
	food.IsActive = false
	food.RespawnAt = now + r.respawnDelayMs

	return food, true
}

// TickRespawns handles periodic respawn of consumed static foods
func (r *StaticFoodRegistry) TickRespawns(nowMs int64) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	respawned := 0
	for i := range r.foods {
		food := &r.foods[i]
		if !food.IsActive && food.RespawnAt > 0 && nowMs >= food.RespawnAt {
			food.IsActive = true
			food.RespawnAt = 0
			respawned++
		}
	}
	return respawned
}

// CountActive returns total active static foods
func (r *StaticFoodRegistry) CountActive() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	active := 0
	for i := range r.foods {
		if r.foods[i].IsActive {
			active++
		}
	}
	return active
}

// GetTotalLoaded returns total foods loaded from dataset
func (r *StaticFoodRegistry) GetTotalLoaded() int {
	return r.totalLoaded
}

// GetFoodsInViewport returns static foods within visible bounding box
func (r *StaticFoodRegistry) GetFoodsInViewport(minX, minY, maxX, maxY float64) []StaticFood {
	r.mu.RLock()
	defer r.mu.RUnlock()

	minGX := int(math.Max(0, math.Floor(minX/r.gridSize)))
	maxGX := int(math.Min(99, math.Floor(maxX/r.gridSize)))
	minGY := int(math.Max(0, math.Floor(minY/r.gridSize)))
	maxGY := int(math.Min(99, math.Floor(maxY/r.gridSize)))

	var result []StaticFood
	for gx := minGX; gx <= maxGX; gx++ {
		for gy := minGY; gy <= maxGY; gy++ {
			for _, food := range r.grid[gx][gy] {
				if food.IsActive {
					result = append(result, *food)
				}
			}
		}
	}
	return result
}


