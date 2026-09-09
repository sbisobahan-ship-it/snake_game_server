package game

import (
	"math/rand"
	"sync"
	"time"
)

type Direction string

const (
	DirUp    Direction = "UP"
	DirDown  Direction = "DOWN"
	DirLeft  Direction = "LEFT"
	DirRight Direction = "RIGHT"
)

type Point struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type Snake struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Color     string    `json:"color"`
	Body      []Point   `json:"body"`
	Direction Direction `json:"direction"`
	NextDir   Direction `json:"nextDir"`
	Score     int       `json:"score"`
	Alive     bool      `json:"alive"`
}

type Food struct {
	X     int    `json:"x"`
	Y     int    `json:"y"`
	Color string `json:"color"`
}

type GameState struct {
	Width       int               `json:"width"`
	Height      int               `json:"height"`
	Snakes      map[string]*Snake `json:"snakes"`
	Foods       []Food            `json:"foods"`
	Leaderboard []LeaderboardItem `json:"leaderboard"`
	TickCount   uint64            `json:"tickCount"`
}

type LeaderboardItem struct {
	Name  string `json:"name"`
	Score int    `json:"score"`
	Color string `json:"color"`
}

type GameEngine struct {
	mu          sync.RWMutex
	Width       int
	Height      int
	Snakes      map[string]*Snake
	Foods       []Food
	TickCount   uint64
	colors      []string
	colorIndex  int
	OnStateTick func(state GameState)
	stopChan    chan struct{}
}

func NewGameEngine(width, height int) *GameEngine {
	ge := &GameEngine{
		Width:  width,
		Height: height,
		Snakes: make(map[string]*Snake),
		Foods:  make([]Food, 0),
		colors: []string{
			"#00ffcc", "#ff007f", "#ffe600", "#00d2ff",
			"#a855f7", "#22c55e", "#f97316", "#ec4899",
		},
		stopChan: make(chan struct{}),
	}

	// Spawn initial food
	ge.spawnFoods(5)
	return ge
}

func (ge *GameEngine) Start() {
	ticker := time.NewTicker(100 * time.Millisecond) // 10 ticks per second
	go func() {
		for {
			select {
			case <-ticker.C:
				ge.Tick()
			case <-ge.stopChan:
				ticker.Stop()
				return
			}
		}
	}()
}

func (ge *GameEngine) Stop() {
	close(ge.stopChan)
}

func (ge *GameEngine) AddPlayer(id, name string) *Snake {
	ge.mu.Lock()
	defer ge.mu.Unlock()

	color := ge.colors[ge.colorIndex%len(ge.colors)]
	ge.colorIndex++

	startX := rand.Intn(ge.Width-10) + 5
	startY := rand.Intn(ge.Height-10) + 5

	snake := &Snake{
		ID:        id,
		Name:      name,
		Color:     color,
		Direction: DirRight,
		NextDir:   DirRight,
		Score:     0,
		Alive:     true,
		Body: []Point{
			{X: startX, Y: startY},
			{X: startX - 1, Y: startY},
			{X: startX - 2, Y: startY},
		},
	}

	ge.Snakes[id] = snake
	return snake
}

func (ge *GameEngine) RemovePlayer(id string) {
	ge.mu.Lock()
	defer ge.mu.Unlock()
	delete(ge.Snakes, id)
}

func (ge *GameEngine) SetDirection(id string, dir Direction) {
	ge.mu.Lock()
	defer ge.mu.Unlock()

	snake, exists := ge.Snakes[id]
	if !exists || !snake.Alive {
		return
	}

	// Prevent 180-degree immediate turns into own body
	if (dir == DirUp && snake.Direction == DirDown) ||
		(dir == DirDown && snake.Direction == DirUp) ||
		(dir == DirLeft && snake.Direction == DirRight) ||
		(dir == DirRight && snake.Direction == DirLeft) {
		return
	}

	snake.NextDir = dir
}

func (ge *GameEngine) RespawnPlayer(id string) {
	ge.mu.Lock()
	defer ge.mu.Unlock()

	snake, exists := ge.Snakes[id]
	if !exists {
		return
	}

	startX := rand.Intn(ge.Width-10) + 5
	startY := rand.Intn(ge.Height-10) + 5

	snake.Direction = DirRight
	snake.NextDir = DirRight
	snake.Score = 0
	snake.Alive = true
	snake.Body = []Point{
		{X: startX, Y: startY},
		{X: startX - 1, Y: startY},
		{X: startX - 2, Y: startY},
	}
}

func (ge *GameEngine) spawnFoods(count int) {
	for i := 0; i < count; i++ {
		food := Food{
			X:     rand.Intn(ge.Width),
			Y:     rand.Intn(ge.Height),
			Color: "#ff3366",
		}
		ge.Foods = append(ge.Foods, food)
	}
}

func (ge *GameEngine) Tick() {
	ge.mu.Lock()
	ge.TickCount++

	// 1. Move snakes
	for _, snake := range ge.Snakes {
		if !snake.Alive {
			continue
		}

		snake.Direction = snake.NextDir
		head := snake.Body[0]
		newHead := head

		switch snake.Direction {
		case DirUp:
			newHead.Y--
		case DirDown:
			newHead.Y++
		case DirLeft:
			newHead.X--
		case DirRight:
			newHead.X++
		}

		// Wall collision - wrap around or die (wrap around is smoother in multiplayer)
		if newHead.X < 0 {
			newHead.X = ge.Width - 1
		} else if newHead.X >= ge.Width {
			newHead.X = 0
		}
		if newHead.Y < 0 {
			newHead.Y = ge.Height - 1
		} else if newHead.Y >= ge.Height {
			newHead.Y = 0
		}

		// Self & Other snake collision detection
		collision := false
		for _, other := range ge.Snakes {
			if !other.Alive {
				continue
			}
			startIndex := 0
			if other.ID == snake.ID {
				startIndex = 1 // don't check against current head
			}
			for i := startIndex; i < len(other.Body); i++ {
				if other.Body[i].X == newHead.X && other.Body[i].Y == newHead.Y {
					collision = true
					break
				}
			}
			if collision {
				break
			}
		}

		if collision {
			snake.Alive = false
			// Turn dead snake body into food pellets
			for _, pt := range snake.Body {
				if rand.Float32() < 0.5 {
					ge.Foods = append(ge.Foods, Food{
						X:     pt.X,
						Y:     pt.Y,
						Color: "#ffe600",
					})
				}
			}
			continue
		}

		// Check food collision
		ateFoodIndex := -1
		for i, food := range ge.Foods {
			if food.X == newHead.X && food.Y == newHead.Y {
				ateFoodIndex = i
				break
			}
		}

		// Prepend new head
		snake.Body = append([]Point{newHead}, snake.Body...)

		if ateFoodIndex != -1 {
			// Ate food: remove food, increase score, keep tail (grows)
			ge.Foods = append(ge.Foods[:ateFoodIndex], ge.Foods[ateFoodIndex+1:]...)
			snake.Score += 10
		} else {
			// Didn't eat food: remove tail
			snake.Body = snake.Body[:len(snake.Body)-1]
		}
	}

	// Keep minimum foods on map
	if len(ge.Foods) < 8 {
		ge.spawnFoods(8 - len(ge.Foods))
	}

	state := ge.getStateSnapshot()
	ge.mu.Unlock()

	if ge.OnStateTick != nil {
		ge.OnStateTick(state)
	}
}

func (ge *GameEngine) getStateSnapshot() GameState {
	snakesCopy := make(map[string]*Snake, len(ge.Snakes))
	leaderboard := make([]LeaderboardItem, 0, len(ge.Snakes))

	for id, s := range ge.Snakes {
		bodyCopy := make([]Point, len(s.Body))
		copy(bodyCopy, s.Body)

		snakesCopy[id] = &Snake{
			ID:        s.ID,
			Name:      s.Name,
			Color:     s.Color,
			Body:      bodyCopy,
			Direction: s.Direction,
			NextDir:   s.NextDir,
			Score:     s.Score,
			Alive:     s.Alive,
		}

		leaderboard = append(leaderboard, LeaderboardItem{
			Name:  s.Name,
			Score: s.Score,
			Color: s.Color,
		})
	}

	// Sort leaderboard descending
	for i := 0; i < len(leaderboard); i++ {
		for j := i + 1; j < len(leaderboard); j++ {
			if leaderboard[j].Score > leaderboard[i].Score {
				leaderboard[i], leaderboard[j] = leaderboard[j], leaderboard[i]
			}
		}
	}

	foodsCopy := make([]Food, len(ge.Foods))
	copy(foodsCopy, ge.Foods)

	return GameState{
		Width:       ge.Width,
		Height:      ge.Height,
		Snakes:      snakesCopy,
		Foods:       foodsCopy,
		Leaderboard: leaderboard,
		TickCount:   ge.TickCount,
	}
}
