package config

import (
	"os"
	"strconv"
)

// Config holds all server and game engine settings
type Config struct {
	Port              string
	TickRate          int     // Server ticks per second (30 or 60 TPS)
	WorldWidth        float64 // Arena width (e.g., 30000.0)
	WorldHeight       float64 // Arena height (e.g., 30000.0)
	BorderThickness   float64 // Border thickness / brick margin (e.g., 220.0)
	MaxPlayers        int     // Max players per room
	StaticFoodCSVPath string  // Path to 10k static foods CSV
	GridCellSize      float64 // Spatial partition grid size (e.g. 300.0)
}

// Load loads configuration with environment variable fallbacks
func Load() *Config {
	port := getEnv("PORT", "8080")
	tickRate := getEnvAsInt("TICK_RATE", 30)
	worldWidth := getEnvAsFloat("WORLD_WIDTH", 30000.0)
	worldHeight := getEnvAsFloat("WORLD_HEIGHT", 30000.0)
	borderThickness := getEnvAsFloat("BORDER_THICKNESS", 220.0)
	maxPlayers := getEnvAsInt("MAX_PLAYERS", 100)
	staticFoodCSVPath := getEnv("STATIC_FOOD_CSV", "data/world_static_foods_10k.csv")
	gridCellSize := getEnvAsFloat("GRID_CELL_SIZE", 300.0)

	return &Config{
		Port:              port,
		TickRate:          tickRate,
		WorldWidth:        worldWidth,
		WorldHeight:       worldHeight,
		BorderThickness:   borderThickness,
		MaxPlayers:        maxPlayers,
		StaticFoodCSVPath: staticFoodCSVPath,
		GridCellSize:      gridCellSize,
	}
}

func getEnv(key, defaultVal string) string {
	if val, exists := os.LookupEnv(key); exists {
		return val
	}
	return defaultVal
}

func getEnvAsInt(key string, defaultVal int) int {
	if val, exists := os.LookupEnv(key); exists {
		if intVal, err := strconv.Atoi(val); err == nil {
			return intVal
		}
	}
	return defaultVal
}

func getEnvAsFloat(key string, defaultVal float64) float64 {
	if val, exists := os.LookupEnv(key); exists {
		if floatVal, err := strconv.ParseFloat(val, 64); err == nil {
			return floatVal
		}
	}
	return defaultVal
}
