package game

import (
	"math"
	"sync"
)

const (
	GridWorldWidth  = 30000.0
	GridWorldHeight = 30000.0
	GridCellSize    = 500.0
	GridCols        = 60 // 30000 / 500
	GridRows        = 60 // 30000 / 500
	TotalCells      = GridCols * GridRows // 3600
)

// FoodItem represents a single food orb in the spatial partitioning grid
type FoodItem struct {
	ID         uint32  `json:"id"`
	X          float32 `json:"x"`
	Y          float32 `json:"y"`
	ColorIndex uint8   `json:"color_index,omitempty"`
	FruitIndex uint8   `json:"fruit_index,omitempty"`
	Value      uint16  `json:"val,omitempty"`
	Radius     float32 `json:"radius,omitempty"`
}

// SpatialFoodGrid partitions the 30kx30k world into 3,600 spatial cells (60x60)
// for ultra-fast AoI / viewport queries and O(1) removals.
type SpatialFoodGrid struct {
	mu          sync.RWMutex
	cells       [TotalCells]map[uint32]*FoodItem
	foodCellMap map[uint32]int
}

// NewSpatialFoodGrid creates and initializes an empty 60x60 spatial food grid
func NewSpatialFoodGrid() *SpatialFoodGrid {
	grid := &SpatialFoodGrid{
		foodCellMap: make(map[uint32]int),
	}
	for i := 0; i < TotalCells; i++ {
		grid.cells[i] = make(map[uint32]*FoodItem)
	}
	return grid
}

// GetCellIndex calculates the 1D flat cell index [0..3599] for given (x, y) coordinates
// Formula: index = (floor(y / 500) * 60) + floor(x / 500)
func GetCellIndex(x, y float32) int {
	col := int(math.Floor(float64(x) / GridCellSize))
	row := int(math.Floor(float64(y) / GridCellSize))

	if col < 0 {
		col = 0
	} else if col >= GridCols {
		col = GridCols - 1
	}

	if row < 0 {
		row = 0
	} else if row >= GridRows {
		row = GridRows - 1
	}

	return row*GridCols + col
}

// IndexFoods clears existing data and populates all 3,600 cells with the provided food items
func (g *SpatialFoodGrid) IndexFoods(foods []*FoodItem) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.foodCellMap = make(map[uint32]int, len(foods))
	for i := 0; i < TotalCells; i++ {
		g.cells[i] = make(map[uint32]*FoodItem)
	}

	for _, food := range foods {
		if food == nil {
			continue
		}
		idx := GetCellIndex(food.X, food.Y)
		g.cells[idx][food.ID] = food
		g.foodCellMap[food.ID] = idx
	}
}

// InsertFood dynamically adds or updates a single food item in the spatial grid
func (g *SpatialFoodGrid) InsertFood(food *FoodItem) {
	if food == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	// If food already exists in a different cell, remove it from old cell
	if oldIdx, exists := g.foodCellMap[food.ID]; exists {
		delete(g.cells[oldIdx], food.ID)
	}

	idx := GetCellIndex(food.X, food.Y)
	g.cells[idx][food.ID] = food
	g.foodCellMap[food.ID] = idx
}

// RemoveFood performs an instant O(1) removal when a player eats a food item
func (g *SpatialFoodGrid) RemoveFood(foodID uint32) *FoodItem {
	g.mu.Lock()
	defer g.mu.Unlock()

	cellIdx, exists := g.foodCellMap[foodID]
	if !exists {
		return nil
	}

	item := g.cells[cellIdx][foodID]
	delete(g.cells[cellIdx], foodID)
	delete(g.foodCellMap, foodID)

	return item
}

// GetFood retrieves a food item by ID in O(1) time
func (g *SpatialFoodGrid) GetFood(foodID uint32) *FoodItem {
	g.mu.RLock()
	defer g.mu.RUnlock()

	cellIdx, exists := g.foodCellMap[foodID]
	if !exists {
		return nil
	}
	return g.cells[cellIdx][foodID]
}

// QueryArea efficiently retrieves only the food items within the bounding box (player viewport)
// without iterating over all 10,000+ foods in the world.
func (g *SpatialFoodGrid) QueryArea(left, top, right, bottom float32) []*FoodItem {
	minX := left
	maxX := right
	if minX > maxX {
		minX, maxX = maxX, minX
	}

	minY := top
	maxY := bottom
	if minY > maxY {
		minY, maxY = maxY, minY
	}

	// Calculate bounding cell indices
	minCol := int(math.Floor(float64(minX) / GridCellSize))
	maxCol := int(math.Floor(float64(maxX) / GridCellSize))
	minRow := int(math.Floor(float64(minY) / GridCellSize))
	maxRow := int(math.Floor(float64(maxY) / GridCellSize))

	if minCol < 0 {
		minCol = 0
	}
	if maxCol >= GridCols {
		maxCol = GridCols - 1
	}
	if minRow < 0 {
		minRow = 0
	}
	if maxRow >= GridRows {
		maxRow = GridRows - 1
	}

	g.mu.RLock()
	defer g.mu.RUnlock()

	var result []*FoodItem

	for r := minRow; r <= maxRow; r++ {
		rowOffset := r * GridCols
		for c := minCol; c <= maxCol; c++ {
			cellIdx := rowOffset + c
			cell := g.cells[cellIdx]
			for _, item := range cell {
				if item.X >= minX && item.X <= maxX && item.Y >= minY && item.Y <= maxY {
					result = append(result, item)
				}
			}
		}
	}

	return result
}

// Count returns the total number of active food items in the grid
func (g *SpatialFoodGrid) Count() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.foodCellMap)
}
