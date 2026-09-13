package game

import (
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"snake_game_server/internal/config"
	"snake_game_server/internal/monitor"
	"snake_game_server/internal/physics"
)

// PlayerDTO represents lightweight player state for transport
type PlayerDTO struct {
	ID      string             `json:"id"`
	Name    string             `json:"name"`
	Head    physics.Vector2D   `json:"head"`
	Angle   float64            `json:"angle"`
	Body    []physics.Vector2D `json:"body"`
	Score   int                `json:"score"`
	IsAlive bool               `json:"alive"`
	SkinID  int                `json:"skin"`
	IsBoost bool               `json:"boost"`
}

// FoodEatenEvent represents a food consumed during this tick frame batch
type FoodEatenEvent struct {
	FoodID     uint32 `json:"id"`
	ColorIndex uint8  `json:"c,omitempty"`
	EaterID    string `json:"e"`     // Player who claimed and ate it
	NewScore   int    `json:"score"` // Total score after eating
}

// FoodSpawnEvent represents a food spawned or dropped during this tick frame batch
type FoodSpawnEvent struct {
	FoodID     uint32  `json:"id"`
	ColorIndex uint8   `json:"c"`
	X          float32 `json:"x"`
	Y          float32 `json:"y"`
	Value      uint16  `json:"v"`
}

// WorldState represents the complete single snapshot block sent per FPS/Tick
type WorldState struct {
	Timestamp    int64            `json:"ts"`
	Players      []PlayerDTO      `json:"players"`
	EatenFoods   []FoodEatenEvent `json:"eaten,omitempty"`   // Batched eaten foods for this 33ms frame
	SpawnedFoods []FoodSpawnEvent `json:"spawned,omitempty"` // Batched spawned foods for this 33ms frame
}

// consumedFoodEntry stores timestamped food ID for O(1) FIFO expiration
type consumedFoodEntry struct {
	foodID   uint32
	expireAt int64
}

// EatRequest represents a fast queued eat event from any connected client
type EatRequest struct {
	PlayerID  string
	FoodID    uint32
	ScoreGain int
}

// Room manages an active match / arena instance with authoritative tick loop
type Room struct {
	ID              string
	Config          *config.Config
	Players         map[string]*Player
	Foods           map[uint32]*Food
	consumedFoodIDs map[uint32]int64    // Fast lookup: foodID -> expireAt timestamp (ms)
	consumedQueue   []consumedFoodEntry // Ordered FIFO queue for O(1) instant head eviction
	pendingEaten    []FoodEatenEvent    // Batched eaten foods in the current tick window
	pendingSpawned  []FoodSpawnEvent    // Batched spawned foods in the current tick window
	eatQueue        chan EatRequest     // High-concurrency lock-free eat ingestion queue (capacity: 16384)
	foodSeq         uint32
	maxFoods        int
	totalEaten      uint64
	mu              sync.RWMutex
	broadcastFn     func(state *WorldState)
	onEatBatchFn    func(events []FoodEatenEvent)
	isRunning       bool
	stopChan        chan struct{}
}

// NewRoom creates a new game room instance
func NewRoom(id string, cfg *config.Config, broadcastFn func(state *WorldState)) *Room {
	r := &Room{
		ID:              id,
		Config:          cfg,
		Players:         make(map[string]*Player),
		Foods:           make(map[uint32]*Food),
		consumedFoodIDs: make(map[uint32]int64, 4096),
		consumedQueue:   make([]consumedFoodEntry, 0, 512),
		eatQueue:        make(chan EatRequest, 16384),
		maxFoods:        250,
		broadcastFn:     broadcastFn,
		stopChan:        make(chan struct{}),
	}

	r.populateInitialFoods()

	monitor.DefaultHub.Emit(monitor.ChanPhysics, "info", "Room '%s' initialized (World: %.0fx%.0f, Target TPS: %d)", id, cfg.WorldWidth, cfg.WorldHeight, cfg.TickRate)

	return r
}

func (r *Room) populateInitialFoods() {
	halfW := (r.Config.WorldWidth / 2.0) * 0.95
	halfH := (r.Config.WorldHeight / 2.0) * 0.95

	for i := 0; i < r.maxFoods; i++ {
		pos := physics.Vector2D{
			X: (rand.Float64()*2 - 1) * halfW,
			Y: (rand.Float64()*2 - 1) * halfH,
		}
		id := atomic.AddUint32(&r.foodSeq, 1)
		val := rand.Intn(3) + 1
		radius := 8.0 + float64(val)*2.0
		colorIndex := uint8(rand.Intn(8))
		food := NewFood(id, pos, val, radius, colorIndex, false)
		r.Foods[id] = food
	}

	monitor.DefaultHub.Emit(monitor.ChanFood, "spawn", "🍎 Spawned %d initial food orbs across the arena", r.maxFoods)
}

// SpawnCustomFoods spawns additional foods dynamically
func (r *Room) SpawnCustomFoods(count int) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	halfW := (r.Config.WorldWidth / 2.0) * 0.95
	halfH := (r.Config.WorldHeight / 2.0) * 0.95

	for i := 0; i < count; i++ {
		pos := physics.Vector2D{
			X: (rand.Float64()*2 - 1) * halfW,
			Y: (rand.Float64()*2 - 1) * halfH,
		}
		id := atomic.AddUint32(&r.foodSeq, 1)
		val := rand.Intn(3) + 1
		radius := 8.0 + float64(val)*2.0
		colorIndex := uint8(rand.Intn(8))
		f := NewFood(id, pos, val, radius, colorIndex, false)
		r.Foods[id] = f

		r.pendingSpawned = append(r.pendingSpawned, FoodSpawnEvent{
			FoodID:     f.ID,
			ColorIndex: f.ColorIndex,
			X:          float32(f.Pos.X),
			Y:          float32(f.Pos.Y),
			Value:      uint16(f.Value),
		})
	}

	monitor.DefaultHub.Emit(monitor.ChanFood, "spawn", "✨ Dynamically spawned %d new food orbs (Total Active: %d)", count, len(r.Foods))
	return len(r.Foods)
}

// AddPlayer adds a new player with a random spawn location
func (r *Room) AddPlayer(id, name string, skinID int) *Player {
	r.mu.Lock()
	defer r.mu.Unlock()

	halfW := (r.Config.WorldWidth / 2.0) * 0.6
	halfH := (r.Config.WorldHeight / 2.0) * 0.6

	spawnPos := physics.Vector2D{
		X: (rand.Float64()*2 - 1) * halfW,
		Y: (rand.Float64()*2 - 1) * halfH,
	}
	angle := rand.Float64() * 2 * 3.1415926535

	player := NewPlayer(id, name, spawnPos, angle, skinID)
	r.Players[id] = player

	monitor.DefaultHub.Emit(monitor.ChanPlayer, "success", "🎮 [PLAYER JOINED] ID: %s | Name: '%s' | Skin: %d | Spawn: (%.0f, %.0f)", id, name, skinID, spawnPos.X, spawnPos.Y)

	return player
}

// RemovePlayer cleans up player on disconnect
func (r *Room) RemovePlayer(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if p, exists := r.Players[id]; exists {
		monitor.DefaultHub.Emit(monitor.ChanPlayer, "warn", "👋 [PLAYER LEFT] ID: %s | Name: '%s' | Final Score: %d", id, p.Name, p.Snake.Score)
		delete(r.Players, id)
	}
}

// UpdatePlayerInput applies received client input (target angle and boost)
func (r *Room) UpdatePlayerInput(playerID string, targetAngle float64, isBoosting bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if p, exists := r.Players[playerID]; exists && p.Snake != nil && p.Snake.IsAlive {
		p.Snake.TargetAngle = targetAngle
		p.Snake.IsBoosting = isBoosting
	}
}

func (r *Room) markFoodConsumedLocked(foodID uint32, now int64) {
	expireAt := now + 8000 // 8 seconds TTL
	r.consumedFoodIDs[foodID] = expireAt
	r.consumedQueue = append(r.consumedQueue, consumedFoodEntry{
		foodID:   foodID,
		expireAt: expireAt,
	})
	delete(r.Foods, foodID)
	atomic.AddUint64(&r.totalEaten, 1)
}

func (r *Room) isFoodConsumedLocked(foodID uint32, now int64) bool {
	if exp, exists := r.consumedFoodIDs[foodID]; exists {
		if now < exp {
			return true
		}
		// Lazy cleanup if accessed after expiration
		delete(r.consumedFoodIDs, foodID)
	}
	return false
}

func (r *Room) evictExpiredFoodsLocked(now int64) {
	var popped int
	for _, entry := range r.consumedQueue {
		if entry.expireAt > now {
			// Chronological order guarantee: First unexpired item means all subsequent items are unexpired.
			break
		}
		if exp, exists := r.consumedFoodIDs[entry.foodID]; exists && exp <= now {
			delete(r.consumedFoodIDs, entry.foodID)
		}
		popped++
	}

	if popped > 0 {
		r.consumedQueue = r.consumedQueue[popped:]
		if len(r.consumedQueue) == 0 && cap(r.consumedQueue) > 1024 {
			r.consumedQueue = make([]consumedFoodEntry, 0, 512)
		}
	}
}

// SetEatBatchCallback registers the callback invoked when a batch of foods is consumed
func (r *Room) SetEatBatchCallback(fn func(events []FoodEatenEvent)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onEatBatchFn = fn
}

// EnqueueEat adds an incoming eat request into the high-speed lock-free channel.
// Extremely fast (nanoseconds), non-blocking, safe for 150+ concurrent players.
func (r *Room) EnqueueEat(playerID string, foodID uint32, scoreGain int) {
	if scoreGain <= 0 {
		scoreGain = 1
	}
	select {
	case r.eatQueue <- EatRequest{PlayerID: playerID, FoodID: foodID, ScoreGain: scoreGain}:
	default:
		// Queue saturated under abnormal load
	}
}

// FlushEatBatch drains all queued eat requests in a single atomic batch,
// applies authoritative game logic, and triggers binary block broadcast to all players.
func (r *Room) FlushEatBatch() []FoodEatenEvent {
	qLen := len(r.eatQueue)
	if qLen == 0 {
		return nil
	}

	r.mu.Lock()
	events := r.flushEatBatchLocked(time.Now().UnixMilli())
	batchFn := r.onEatBatchFn
	r.mu.Unlock()

	if len(events) > 0 && batchFn != nil {
		batchFn(events)
	}
	return events
}

func (r *Room) flushEatBatchLocked(now int64) []FoodEatenEvent {
	qLen := len(r.eatQueue)
	if qLen == 0 {
		return nil
	}

	accepted := make([]FoodEatenEvent, 0, qLen)
	for i := 0; i < qLen; i++ {
		select {
		case req := <-r.eatQueue:
			if r.isFoodConsumedLocked(req.FoodID, now) {
				continue
			}
			player, exists := r.Players[req.PlayerID]
			if !exists || player.Snake == nil || !player.Snake.IsAlive {
				continue
			}

			r.markFoodConsumedLocked(req.FoodID, now)
			player.Snake.Grow(req.ScoreGain)
			newScore := player.Snake.Score

			event := FoodEatenEvent{
				FoodID:   req.FoodID,
				EaterID:  req.PlayerID,
				NewScore: newScore,
			}
			accepted = append(accepted, event)
			r.pendingEaten = append(r.pendingEaten, event)
		default:
			break
		}
	}
	return accepted
}

// ClaimAndEatFood authoritatively resolves race condition for a food item (First-Come, First-Served)
// If multiple players send eat events for the same FoodID in the same millisecond/frame, only the first arrival gets it.
func (r *Room) ClaimAndEatFood(playerID string, foodID uint32, scoreGain int) (accepted bool, newScore int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now().UnixMilli()

	// Check if already claimed/eaten (active within 8s TTL window)
	if r.isFoodConsumedLocked(foodID, now) {
		return false, 0
	}

	player, exists := r.Players[playerID]
	if !exists || player.Snake == nil || !player.Snake.IsAlive {
		return false, 0
	}

	// Mark food as consumed (adds to map and FIFO queue)
	r.markFoodConsumedLocked(foodID, now)

	if scoreGain <= 0 {
		scoreGain = 1
	}

	player.Snake.Grow(scoreGain)
	newScore = player.Snake.Score

	// Add to frame batch
	r.pendingEaten = append(r.pendingEaten, FoodEatenEvent{
		FoodID:   foodID,
		EaterID:  playerID,
		NewScore: newScore,
	})

	return true, newScore
}

// EatFood removes food immediately and batches removal event for all clients
func (r *Room) EatFood(playerID string, foodID uint32) (colorIndex uint8, scoreGained int, newScore int, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now().UnixMilli()

	if r.isFoodConsumedLocked(foodID, now) {
		return 0, 0, 0, false
	}

	player, exists := r.Players[playerID]
	if !exists || player.Snake == nil || !player.Snake.IsAlive {
		return 0, 0, 0, false
	}

	food, exists := r.Foods[foodID]
	if exists {
		scoreGained = food.Value
		colorIndex = food.ColorIndex
	} else {
		scoreGained = 1
		colorIndex = 0
	}

	r.markFoodConsumedLocked(foodID, now)

	if scoreGained <= 0 {
		scoreGained = 1
	}

	player.Snake.Grow(scoreGained)
	newScore = player.Snake.Score

	r.pendingEaten = append(r.pendingEaten, FoodEatenEvent{
		FoodID:     foodID,
		ColorIndex: colorIndex,
		EaterID:    playerID,
		NewScore:   newScore,
	})

	return colorIndex, scoreGained, newScore, true
}

// GetAllFoodsDTO returns snapshot list of all currently active foods
func (r *Room) GetAllFoodsDTO() []FoodDTO {
	r.mu.RLock()
	defer r.mu.RUnlock()

	list := make([]FoodDTO, 0, len(r.Foods))
	for _, f := range r.Foods {
		list = append(list, f.ToDTO())
	}
	return list
}

// Tick executes a single game simulation frame (at 30 FPS / TPS)
func (r *Room) Tick(dt float64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 1. Update all snakes position
	for _, p := range r.Players {
		if p.Snake != nil && p.Snake.IsAlive {
			p.Snake.UpdatePosition(dt)
		}
	}

	// 2. Collision with Boundaries
	var diedPlayers []*Player
	for _, p := range r.Players {
		s := p.Snake
		if s == nil || !s.IsAlive {
			continue
		}

		if CheckBoundaryCollision(s.Head, s.HeadRadius, r.Config.WorldWidth, r.Config.WorldHeight) {
			s.IsAlive = false
			diedPlayers = append(diedPlayers, p)
			monitor.DefaultHub.Emit(monitor.ChanPhysics, "warn", "💥 [BORDER CRASH] Player '%s' hit boundary at (%.1f, %.1f)! Snake died.", p.Name, s.Head.X, s.Head.Y)
		}
	}

	// 3. Collision between snakes (Head to other snake body)
	for idA, pA := range r.Players {
		sA := pA.Snake
		if sA == nil || !sA.IsAlive {
			continue
		}

		for idB, pB := range r.Players {
			if idA == idB {
				continue
			}
			sB := pB.Snake
			if sB == nil || !sB.IsAlive {
				continue
			}

			if CheckSnakeBodyCollision(sA.Head, sA.HeadRadius, sB) {
				sA.IsAlive = false
				diedPlayers = append(diedPlayers, pA)
				monitor.DefaultHub.Emit(monitor.ChanPhysics, "error", "⚔️ [SNAKE COLLISION] '%s' crashed into '%s' body! Snake destroyed (Score was: %d).", pA.Name, pB.Name, sA.Score)
				break
			}
		}
	}

	// 4. Handle deaths & convert dead snake bodies into dropped foods
	for _, p := range diedPlayers {
		if p.Snake != nil {
			r.spawnDeathFoodsLocked(p.Snake, p.Name)
		}
	}

	// 5. Server-side Proximity Food Eating (Collision check)
	for _, p := range r.Players {
		s := p.Snake
		if s == nil || !s.IsAlive {
			continue
		}

		for fid, food := range r.Foods {
			if CheckFoodCollision(s.Head, s.HeadRadius, food) {
				r.markFoodConsumedLocked(fid, time.Now().UnixMilli())

				scoreGain := food.Value
				if scoreGain <= 0 {
					scoreGain = 1
				}
				s.Grow(scoreGain)

				r.pendingEaten = append(r.pendingEaten, FoodEatenEvent{
					FoodID:     food.ID,
					ColorIndex: food.ColorIndex,
					EaterID:    p.ID,
					NewScore:   s.Score,
				})
			}
		}
	}

	// 6. Assemble and Broadcast Unified Snapshot Block (Players + Eaten Batch + Spawned Batch)
	if r.broadcastFn != nil {
		state := r.getWorldStateLocked()
		r.broadcastFn(state)
	}

	// Clear frame batches after broadcasting
	r.pendingEaten = nil
	r.pendingSpawned = nil

	// 7. Instant O(1) FIFO Automatic Expiration (Pops expired items directly from the queue head)
	r.evictExpiredFoodsLocked(time.Now().UnixMilli())
}

func (r *Room) spawnDeathFoodsLocked(snake *Snake, playerName string) {
	step := 2
	spawnCount := 0
	for i := 0; i < len(snake.Body); i += step {
		pos := snake.Body[i]
		pos.X += (rand.Float64()*2 - 1) * 10
		pos.Y += (rand.Float64()*2 - 1) * 10

		id := atomic.AddUint32(&r.foodSeq, 1)
		val := 5
		radius := 12.0
		colorIndex := uint8(rand.Intn(8))
		f := NewFood(id, pos, val, radius, colorIndex, true)
		r.Foods[id] = f

		r.pendingSpawned = append(r.pendingSpawned, FoodSpawnEvent{
			FoodID:     f.ID,
			ColorIndex: f.ColorIndex,
			X:          float32(f.Pos.X),
			Y:          float32(f.Pos.Y),
			Value:      uint16(f.Value),
		})
		spawnCount++
	}

	monitor.DefaultHub.Emit(monitor.ChanFood, "spawn", "✨ [DEATH FOOD BURST] Spawned %d death food orbs from '%s'", spawnCount, playerName)
}

// getWorldStateLocked generates snapshot DTO containing all frame data
func (r *Room) getWorldStateLocked() *WorldState {
	playerDTOs := make([]PlayerDTO, 0, len(r.Players))
	for _, p := range r.Players {
		if p.Snake == nil {
			continue
		}
		playerDTOs = append(playerDTOs, PlayerDTO{
			ID:      p.ID,
			Name:    p.Name,
			Head:    p.Snake.Head,
			Angle:   p.Snake.Angle,
			Body:    p.Snake.Body,
			Score:   p.Snake.Score,
			IsAlive: p.Snake.IsAlive,
			SkinID:  p.Snake.SkinID,
			IsBoost: p.Snake.IsBoosting,
		})
	}

	return &WorldState{
		Timestamp:    time.Now().UnixMilli(),
		Players:      playerDTOs,
		EatenFoods:   r.pendingEaten,
		SpawnedFoods: r.pendingSpawned,
	}
}

// GetPlayerCount returns active player count
func (r *Room) GetPlayerCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.Players)
}

// StartGameLoop starts the authoritative room tick loop in a goroutine
func (r *Room) StartGameLoop() {
	r.mu.Lock()
	if r.isRunning {
		r.mu.Unlock()
		return
	}
	r.isRunning = true
	r.mu.Unlock()

	tickDuration := time.Second / time.Duration(r.Config.TickRate)
	ticker := time.NewTicker(tickDuration)
	metricsTicker := time.NewTicker(time.Second)
	eatBatchTicker := time.NewTicker(15 * time.Millisecond) // Ultra-low latency 15ms micro-batching

	go func() {
		lastTime := time.Now()
		for {
			select {
			case <-r.stopChan:
				ticker.Stop()
				metricsTicker.Stop()
				eatBatchTicker.Stop()
				return
			case <-eatBatchTicker.C:
				r.FlushEatBatch()
			case now := <-ticker.C:
				dt := now.Sub(lastTime).Seconds()
				lastTime = now
				r.Tick(dt)
			case <-metricsTicker.C:
				r.mu.RLock()
				pCount := len(r.Players)
				fCount := len(r.Foods)
				totalEaten := atomic.LoadUint64(&r.totalEaten)
				r.mu.RUnlock()
				monitor.DefaultHub.EmitMetrics(pCount, fCount, r.Config.TickRate, totalEaten)
			}
		}
	}()
}

// Stop terminates the room loop
func (r *Room) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.isRunning {
		close(r.stopChan)
		r.isRunning = false
	}
}
