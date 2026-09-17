package game

import (
	crand "crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"math"
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
	ID               string             `json:"id"`
	Token            string             `json:"token,omitempty"`
	Name             string             `json:"name"`
	Head             physics.Vector2D   `json:"head"`
	Angle            float64            `json:"angle"`
	Body             []physics.Vector2D `json:"body"`
	Score            int                `json:"score"`
	IsAlive          bool               `json:"alive"`
	SkinID           int                `json:"skin"`
	IsBoost          bool               `json:"boost"`
	StaticFoodsEaten int                `json:"static_foods_eaten"`
	Segments         int                `json:"segments"`
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

// generateSessionToken generates a cryptographically secure 32-character hex session token
func generateSessionToken() string {
	b := make([]byte, 16)
	if _, err := crand.Read(b); err != nil {
		return fmt.Sprintf("tok_%d_%d", time.Now().UnixNano(), rand.Int63())
	}
	return "tok_" + hex.EncodeToString(b)
}

// Room manages an active match / arena instance with authoritative tick loop
type Room struct {
	ID              string
	Config          *config.Config
	Players         map[string]*Player
	tokenToPlayer   map[string]*Player      // Fast lookup: Token -> Active Living Player
	deadSessions    map[string]*DeadSession // Lookup: Token -> Terminal dead state info
	Foods           map[uint32]*Food
	StaticFoods     *StaticFoodRegistry // 10k static food spatial dataset (30kx30k canvas)
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
	onPlayerDeadFn  func(playerID string, ds *DeadSession)
	isRunning       bool
	stopChan        chan struct{}
}

// DeadEvent represents an authoritative death event on the server
type DeadEvent struct {
	PlayerID string
	Session  *DeadSession
}

// SetPlayerDeadCallback registers the callback invoked when a player dies authoritatively on the server
func (r *Room) SetPlayerDeadCallback(fn func(playerID string, ds *DeadSession)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onPlayerDeadFn = fn
}

// NewRoom creates a new game room instance
func NewRoom(id string, cfg *config.Config, broadcastFn func(state *WorldState)) *Room {
	r := &Room{
		ID:              id,
		Config:          cfg,
		Players:         make(map[string]*Player),
		tokenToPlayer:   make(map[string]*Player),
		deadSessions:    make(map[string]*DeadSession),
		Foods:           make(map[uint32]*Food),
		consumedFoodIDs: make(map[uint32]int64, 4096),
		consumedQueue:   make([]consumedFoodEntry, 0, 512),
		eatQueue:        make(chan EatRequest, 16384),
		maxFoods:        250,
		broadcastFn:     broadcastFn,
		stopChan:        make(chan struct{}),
	}

	if cfg.StaticFoodCSVPath != "" {
		staticFoods, err := NewStaticFoodRegistry(cfg.StaticFoodCSVPath, cfg.GridCellSize)
		if err != nil {
			log.Printf("⚠️ Warning: Could not load static foods CSV (%s): %v. Fallback to dynamic foods.", cfg.StaticFoodCSVPath, err)
			r.populateInitialFoods()
		} else {
			r.StaticFoods = staticFoods
		}
	} else {
		r.populateInitialFoods()
	}

	monitor.DefaultHub.Emit(monitor.ChanPhysics, "info", "Room '%s' initialized (World: %.0fx%.0f, Target TPS: %d)", id, cfg.WorldWidth, cfg.WorldHeight, cfg.TickRate)

	return r
}

func (r *Room) populateInitialFoods() {
	borderMargin := r.Config.BorderThickness
	if borderMargin <= 0 {
		borderMargin = 220.0
	}
	minX := borderMargin + 100.0
	maxX := math.Max(minX, r.Config.WorldWidth-borderMargin-100.0)
	minY := borderMargin + 100.0
	maxY := math.Max(minY, r.Config.WorldHeight-borderMargin-100.0)

	for i := 0; i < r.maxFoods; i++ {
		pos := physics.Vector2D{
			X: minX + rand.Float64()*(maxX-minX),
			Y: minY + rand.Float64()*(maxY-minY),
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

	borderMargin := r.Config.BorderThickness
	if borderMargin <= 0 {
		borderMargin = 220.0
	}
	minX := borderMargin + 100.0
	maxX := math.Max(minX, r.Config.WorldWidth-borderMargin-100.0)
	minY := borderMargin + 100.0
	maxY := math.Max(minY, r.Config.WorldHeight-borderMargin-100.0)

	for i := 0; i < count; i++ {
		pos := physics.Vector2D{
			X: minX + rand.Float64()*(maxX-minX),
			Y: minY + rand.Float64()*(maxY-minY),
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

// AddPlayer adds a new player with a generated token or smoothly reconnects an existing living player
func (r *Room) AddPlayer(id, name string, skinID int) *Player {
	return r.AddPlayerWithToken(id, "", name, skinID)
}

// AddPlayerWithToken adds or reconnects a player with an explicit or newly generated session token
func (r *Room) AddPlayerWithToken(id, token, name string, skinID int) *Player {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 1. If existing living player found by ID or Token, reconnect it!
	if existing, exists := r.Players[id]; exists && existing.Snake != nil && existing.Snake.IsAlive {
		existing.IsConnected = true
		existing.DisconnectedAt = 0
		if name != "" {
			existing.Name = name
		}
		if skinID > 0 {
			existing.Snake.SkinID = skinID
		}
		monitor.DefaultHub.Emit(monitor.ChanPlayer, "success", "🎮 [PLAYER RECONNECTED] ID: %s | Token: %s | Name: '%s' | Live Pos: (%.0f, %.0f) | Score: %d", id, existing.Token, existing.Name, existing.Snake.Head.X, existing.Snake.Head.Y, existing.Snake.Score)
		return existing
	}

	if token != "" {
		if existing, exists := r.tokenToPlayer[token]; exists && existing.Snake != nil && existing.Snake.IsAlive {
			existing.IsConnected = true
			existing.DisconnectedAt = 0
			if name != "" {
				existing.Name = name
			}
			monitor.DefaultHub.Emit(monitor.ChanPlayer, "success", "🎮 [PLAYER RECONNECTED BY TOKEN] ID: %s | Token: %s | Name: '%s' | Live Pos: (%.0f, %.0f) | Score: %d", existing.ID, token, existing.Name, existing.Snake.Head.X, existing.Snake.Head.Y, existing.Snake.Score)
			return existing
		}
	}

	// 2. Generate a new session token if none provided
	if token == "" {
		token = generateSessionToken()
	}

	// Clean up any old dead session record for this token if starting fresh
	delete(r.deadSessions, token)

	borderMargin := r.Config.BorderThickness
	if borderMargin <= 0 {
		borderMargin = 220.0
	}
	safeMargin := borderMargin + 1000.0
	if r.Config.WorldWidth <= (safeMargin * 2) {
		safeMargin = borderMargin + 50.0
	}
	minX := safeMargin
	maxX := math.Max(minX, r.Config.WorldWidth-safeMargin)

	safeMarginH := borderMargin + 1000.0
	if r.Config.WorldHeight <= (safeMarginH * 2) {
		safeMarginH = borderMargin + 50.0
	}
	minY := safeMarginH
	maxY := math.Max(minY, r.Config.WorldHeight-safeMarginH)

	spawnPos := physics.Vector2D{
		X: minX + rand.Float64()*(maxX-minX),
		Y: minY + rand.Float64()*(maxY-minY),
	}
	angle := rand.Float64() * 2 * math.Pi

	player := NewPlayer(id, token, name, spawnPos, angle, skinID)
	r.Players[id] = player
	r.tokenToPlayer[token] = player

	monitor.DefaultHub.Emit(monitor.ChanPlayer, "success", "🎮 [PLAYER JOINED] ID: %s | Token: %s | Name: '%s' | Skin: %d | Spawn: (%.0f, %.0f)", id, token, name, skinID, spawnPos.X, spawnPos.Y)

	return player
}

// GetPlayerByToken looks up active living player associated with session token
func (r *Room) GetPlayerByToken(token string) (*Player, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, exists := r.tokenToPlayer[token]
	if !exists || p == nil || p.Snake == nil || !p.Snake.IsAlive {
		return nil, false
	}
	return p, true
}

// GetDeadSession retrieves terminal state info of a dead player session by token
func (r *Room) GetDeadSession(token string) (*DeadSession, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ds, exists := r.deadSessions[token]
	return ds, exists
}

// MarkPlayerDisconnected preserves player entity and lets snake continue moving forward via server dead reckoning
func (r *Room) MarkPlayerDisconnected(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if p, exists := r.Players[id]; exists {
		p.IsConnected = false
		p.DisconnectedAt = time.Now().UnixMilli()
		monitor.DefaultHub.Emit(monitor.ChanPlayer, "warn", "🔌 [PLAYER DISCONNECTED (PRESERVED)] ID: %s | Token: %s | Name: '%s' (Snake continues alive in arena)", id, p.Token, p.Name)
	}
}

// killPlayerLocked authoritatively kills a player, drops death food, stores dead session, and cleans up token mapping
func (r *Room) killPlayerLocked(p *Player, reason string) *DeadSession {
	if p == nil || p.Snake == nil || !p.Snake.IsAlive {
		return nil
	}

	p.Snake.IsAlive = false
	score := p.Snake.Score
	p.DeathReason = reason
	p.DiedAt = time.Now().UnixMilli()

	r.spawnDeathFoodsLocked(p.Snake, p.Name)

	ds := &DeadSession{
		Token:       p.Token,
		PlayerID:    p.ID,
		Name:        p.Name,
		FinalScore:  score,
		DeathReason: reason,
		DiedAt:      p.DiedAt,
	}
	if p.Token != "" {
		r.deadSessions[p.Token] = ds
		delete(r.tokenToPlayer, p.Token)
	}

	monitor.DefaultHub.Emit(monitor.ChanPlayer, "warn", "💀 [AUTHORITATIVE DEATH] Player '%s' (Token: %s) died! Reason: %s | Final Score: %d", p.ID, p.Token, reason, score)

	return ds
}

// checkFatalCollisionsForPlayerLocked evaluates boundary collision and snake-to-snake body/head collisions authoritatively
func (r *Room) checkFatalCollisionsForPlayerLocked(p *Player) (*DeadSession, bool) {
	if p == nil || p.Snake == nil || !p.Snake.IsAlive {
		return nil, false
	}
	s := p.Snake

	// 1. Boundary collision (breached [borderMargin, WorldWidth - borderMargin] x [borderMargin, WorldHeight - borderMargin])
	borderMargin := r.Config.BorderThickness
	if borderMargin <= 0 {
		borderMargin = 220.0
	}
	if CheckBoundaryCollision(s.Head, s.HeadRadius, r.Config.WorldWidth, r.Config.WorldHeight, borderMargin) {
		ds := r.killPlayerLocked(p, "boundary_collision")
		return ds, true
	}

	// 2. Snake vs Snake Collisions (Body and Head collisions)
	for _, other := range r.Players {
		if other.ID == p.ID || other.Snake == nil || !other.Snake.IsAlive {
			continue
		}

		// A. Head-to-Body Collision: Player p's head hits other snake's body segment
		if CheckSnakeBodyCollision(s.Head, s.HeadRadius, other.Snake) {
			targetName := other.Name
			if targetName == "" {
				targetName = other.ID
			}
			reason := fmt.Sprintf("collided_with_%s", targetName)
			ds := r.killPlayerLocked(p, reason)
			return ds, true
		}

		// B. Head-to-Head Collision
		if CheckSnakeHeadCollision(s.Head, s.HeadRadius, other.Snake.Head, other.Snake.HeadRadius) {
			targetName := other.Name
			if targetName == "" {
				targetName = other.ID
			}
			if s.Score < other.Snake.Score {
				reason := fmt.Sprintf("head_collision_lost_to_%s", targetName)
				ds := r.killPlayerLocked(p, reason)
				return ds, true
			} else if s.Score > other.Snake.Score {
				reason := fmt.Sprintf("head_collision_lost_to_%s", p.Name)
				r.killPlayerLocked(other, reason)
			} else {
				reason := fmt.Sprintf("head_to_head_tie_with_%s", targetName)
				r.killPlayerLocked(other, reason)
				ds := r.killPlayerLocked(p, reason)
				return ds, true
			}
		}
	}

	return nil, false
}

// RecordPlayerDeath records terminal session details for dead snake and clears active token mapping
func (r *Room) RecordPlayerDeath(playerID, reason string) *DeadSession {
	r.mu.Lock()
	defer r.mu.Unlock()

	p, exists := r.Players[playerID]
	if !exists {
		return nil
	}
	return r.killPlayerLocked(p, reason)
}

// RemovePlayer cleans up player on explicit leave or defeat and registers dead session
func (r *Room) RemovePlayer(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if p, exists := r.Players[id]; exists {
		score := 0
		if p.Snake != nil {
			p.Snake.IsAlive = false
			score = p.Snake.Score
		}
		if p.Token != "" {
			r.deadSessions[p.Token] = &DeadSession{
				Token:       p.Token,
				PlayerID:    p.ID,
				Name:        p.Name,
				FinalScore:  score,
				DeathReason: "left_or_defeated",
				DiedAt:      time.Now().UnixMilli(),
			}
			delete(r.tokenToPlayer, p.Token)
		}
		monitor.DefaultHub.Emit(monitor.ChanPlayer, "warn", "👋 [PLAYER REMOVED] ID: %s | Name: '%s' | Final Score: %d", id, p.Name, score)
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

// checkCollisionsForPlayerLocked checks both static and dynamic food collisions for a specific snake head
func (r *Room) checkCollisionsForPlayerLocked(p *Player) []FoodEatenEvent {
	if p == nil || p.Snake == nil || !p.Snake.IsAlive {
		return nil
	}
	s := p.Snake
	var eatenList []FoodEatenEvent
	now := time.Now().UnixMilli()

	// 1. Static foods spatial collision (O(1) grid query on 30k canvas)
	if r.StaticFoods != nil {
		eatenEvents := r.StaticFoods.CheckCollisions(p.ID, s.Head.X, s.Head.Y, s.HeadRadius)
		for _, ef := range eatenEvents {
			s.EatStaticFood(1)
			ef.NewScore = s.Score
			r.pendingEaten = append(r.pendingEaten, ef)
			atomic.AddUint64(&r.totalEaten, 1)
			eatenList = append(eatenList, ef)

			monitor.DefaultHub.Emit(monitor.ChanFood, "eat", "🍎 [SERVER COLLISION EAT] Player '%s' ate Static Food #%d | Total Foods: %d -> Score: %d | Segments: %d", p.ID, ef.FoodID, s.StaticFoodsEaten, s.Score, len(s.Body))
		}
	}

	// 2. Dynamic foods collision check (if any exist)
	for fid, food := range r.Foods {
		if CheckFoodCollision(s.Head, s.HeadRadius, food) {
			r.markFoodConsumedLocked(fid, now)

			scoreGain := food.Value
			if scoreGain <= 0 {
				scoreGain = 1
			}
			s.Grow(scoreGain)

			event := FoodEatenEvent{
				FoodID:     food.ID,
				ColorIndex: food.ColorIndex,
				EaterID:    p.ID,
				NewScore:   s.Score,
			}
			r.pendingEaten = append(r.pendingEaten, event)
			eatenList = append(eatenList, event)

			monitor.DefaultHub.Emit(monitor.ChanFood, "eat", "🍎 [SERVER COLLISION EAT] Player '%s' ate Food #%d at (%.0f, %.0f) -> New Score: %d", p.ID, food.ID, s.Head.X, s.Head.Y, s.Score)
		}
	}

	return eatenList
}

// UpdatePlayerLocation updates client position directly from authoritative client stream (custom FPS)
// and immediately evaluates food collision and fatal collisions on the server, broadcasting events in real-time.
func (r *Room) UpdatePlayerLocation(playerID string, x, y, angle float64, isBoosting bool) []FoodEatenEvent {
	r.mu.Lock()
	p, exists := r.Players[playerID]
	if !exists || p.Snake == nil || !p.Snake.IsAlive {
		r.mu.Unlock()
		return nil
	}

	p.Snake.SetDirectLocation(physics.Vector2D{X: x, Y: y}, angle, isBoosting)
	eatenEvents := r.checkCollisionsForPlayerLocked(p)
	ds, wasKilled := r.checkFatalCollisionsForPlayerLocked(p)

	batchFn := r.onEatBatchFn
	deadFn := r.onPlayerDeadFn
	r.mu.Unlock()

	// Immediately push eaten food numbers to all connected clients in real-time
	if len(eatenEvents) > 0 && batchFn != nil {
		batchFn(eatenEvents)
	}

	// Immediately notify death to client
	if wasKilled && ds != nil && deadFn != nil {
		deadFn(playerID, ds)
	}

	return eatenEvents
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
func (r *Room) ClaimAndEatFood(playerID string, foodID uint32, scoreGain int) (accepted bool, newScore int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now().UnixMilli()

	player, exists := r.Players[playerID]
	if !exists || player.Snake == nil || !player.Snake.IsAlive {
		return false, 0
	}

	// 1. Try static food registry first
	if r.StaticFoods != nil {
		if sf, ok := r.StaticFoods.ClaimFood(foodID, playerID); ok {
			player.Snake.EatStaticFood(1)
			newScore = player.Snake.Score

			r.pendingEaten = append(r.pendingEaten, FoodEatenEvent{
				FoodID:     foodID,
				ColorIndex: sf.FruitIndex,
				EaterID:    playerID,
				NewScore:   newScore,
			})
			atomic.AddUint64(&r.totalEaten, 1)
			return true, newScore
		}
	}

	// 2. Dynamic food lookup fallback
	if r.isFoodConsumedLocked(foodID, now) {
		return false, 0
	}

	r.markFoodConsumedLocked(foodID, now)

	if scoreGain <= 0 {
		scoreGain = 1
	}

	player.Snake.Grow(scoreGain)
	newScore = player.Snake.Score

	r.pendingEaten = append(r.pendingEaten, FoodEatenEvent{
		FoodID:   foodID,
		EaterID:  playerID,
		NewScore: newScore,
	})

	return true, newScore
}

// EatFood removes food immediately and batches removal event for all clients
func (r *Room) EatFood(playerID string, foodID uint32) (colorIndex uint8, scoreGained int, newScore int, ok bool) {
	return r.ClaimAndEatFoodWithColor(playerID, foodID, 0)
}

// ClaimAndEatFoodWithColor resolves food claim and returns full color/score metadata
func (r *Room) ClaimAndEatFoodWithColor(playerID string, foodID uint32, scoreGain int) (colorIndex uint8, scoreGained int, newScore int, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now().UnixMilli()

	player, exists := r.Players[playerID]
	if !exists || player.Snake == nil || !player.Snake.IsAlive {
		return 0, 0, 0, false
	}

	if r.StaticFoods != nil {
		if sf, ok := r.StaticFoods.ClaimFood(foodID, playerID); ok {
			player.Snake.EatStaticFood(1)
			newScore = player.Snake.Score

			r.pendingEaten = append(r.pendingEaten, FoodEatenEvent{
				FoodID:     foodID,
				ColorIndex: sf.FruitIndex,
				EaterID:    playerID,
				NewScore:   newScore,
			})
			atomic.AddUint64(&r.totalEaten, 1)
			return sf.FruitIndex, 1, newScore, true
		}
	}

	if r.isFoodConsumedLocked(foodID, now) {
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

	if r.StaticFoods != nil {
		staticList := r.StaticFoods.GetFoodsInViewport(0, 0, r.Config.WorldWidth, r.Config.WorldHeight)
		list := make([]FoodDTO, 0, len(staticList))
		for _, f := range staticList {
			list = append(list, FoodDTO{
				ID:         f.ID,
				ColorIndex: f.FruitIndex,
				X:          float64(f.X),
				Y:          float64(f.Y),
				Val:        int(f.Value),
				Radius:     float64(f.Radius),
			})
		}
		return list
	}

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

	// 1. Update all living snakes position (dead reckoning simulation)
	for _, p := range r.Players {
		if p.Snake != nil && p.Snake.IsAlive {
			p.Snake.UpdatePosition(dt)
		}
	}

	// 2. Fatal collision detection for all living snakes (boundary & snake body/head collisions)
	var deadEvents []DeadEvent
	for _, p := range r.Players {
		if p.Snake != nil && p.Snake.IsAlive {
			if ds, wasKilled := r.checkFatalCollisionsForPlayerLocked(p); wasKilled && ds != nil {
				deadEvents = append(deadEvents, DeadEvent{
					PlayerID: p.ID,
					Session:  ds,
				})
			}
		}
	}

	// 3. Static 10k Food Spatial Dataset Respawns
	if r.StaticFoods != nil {
		r.StaticFoods.TickRespawns(time.Now().UnixMilli())
	}

	// 4. Spatial Collision Detection for all living snakes
	var tickEatenEvents []FoodEatenEvent
	for _, p := range r.Players {
		if p.Snake != nil && p.Snake.IsAlive {
			eaten := r.checkCollisionsForPlayerLocked(p)
			if len(eaten) > 0 {
				tickEatenEvents = append(tickEatenEvents, eaten...)
			}
		}
	}

	// 5. Assemble and Broadcast Unified Snapshot Block (Players + Eaten Batch + Spawned Batch)
	if r.broadcastFn != nil {
		state := r.getWorldStateLocked()
		r.broadcastFn(state)
	}

	// Clear frame batches after broadcasting
	r.pendingEaten = nil
	r.pendingSpawned = nil

	// 6. Instant O(1) FIFO Automatic Expiration for dynamic foods
	r.evictExpiredFoodsLocked(time.Now().UnixMilli())

	// 7. Fire instant authoritative death callbacks to clients
	if len(deadEvents) > 0 && r.onPlayerDeadFn != nil {
		for _, de := range deadEvents {
			r.onPlayerDeadFn(de.PlayerID, de.Session)
		}
	}

	// 8. Immediate real-time broadcast of eaten foods to all clients
	if len(tickEatenEvents) > 0 && r.onEatBatchFn != nil {
		r.onEatBatchFn(tickEatenEvents)
	}
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
			ID:               p.ID,
			Name:             p.Name,
			Head:             p.Snake.Head,
			Angle:            p.Snake.Angle,
			Body:             p.Snake.Body,
			Score:            p.Snake.Score,
			IsAlive:          p.Snake.IsAlive,
			SkinID:           p.Snake.SkinID,
			IsBoost:          p.Snake.IsBoosting,
			StaticFoodsEaten: p.Snake.StaticFoodsEaten,
			Segments:         len(p.Snake.Body),
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

// GetPlayerSync returns authoritative player snapshot for network lag recovery / synchronization
func (r *Room) GetPlayerSync(playerID string) (*PlayerDTO, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, exists := r.Players[playerID]
	if !exists || p.Snake == nil {
		return nil, false
	}

	bodyCopy := make([]physics.Vector2D, len(p.Snake.Body))
	copy(bodyCopy, p.Snake.Body)

	dto := &PlayerDTO{
		ID:               p.ID,
		Token:            p.Token,
		Name:             p.Name,
		Head:             p.Snake.Head,
		Angle:            p.Snake.Angle,
		Body:             bodyCopy,
		Score:            p.Snake.Score,
		IsAlive:          p.Snake.IsAlive,
		SkinID:           p.Snake.SkinID,
		IsBoost:          p.Snake.IsBoosting,
		StaticFoodsEaten: p.Snake.StaticFoodsEaten,
		Segments:         len(p.Snake.Body),
	}
	return dto, true
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
