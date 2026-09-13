package config

import (
	"os"
	"strconv"
)

// Config holds all server and game engine settings
type Config struct {
	Port         string
	TickRate     int     // Server ticks per second (30 or 60 TPS)
	WorldWidth   float64 // Arena width (e.g., 30000.0)
	WorldHeight  float64 // Arena height (e.g., 30000.0)
	MaxPlayers   int     // Max players per room
}

// Load loads configuration with environment variable fallbacks
func Load() *Config {
	port := getEnv("PORT", "8080")
	tickRate := getEnvAsInt("TICK_RATE", 30)
	worldWidth := getEnvAsFloat("WORLD_WIDTH", 30000.0)
	worldHeight := getEnvAsFloat("WORLD_HEIGHT", 300000.0)
	if worldHeight > 30000.0 && worldWidth == 30000.0 {
		worldHeight = 30000.0
	}
	maxPlayers := getEnvAsInt("MAX_PLAYERS", 100)

	return &Config{
		Port:        port,
		TickRate:    tickRate,
		WorldWidth:  worldWidth,
		WorldHeight: worldHeight,
		MaxPlayers:  maxPlayers,
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
