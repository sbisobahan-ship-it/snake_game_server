package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"snake_game_server/internal/config"
	"snake_game_server/internal/game"
	"snake_game_server/internal/monitor"
	"snake_game_server/internal/network"
)

func getLocalIPs() []string {
	var ips []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ips
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				ips = append(ips, ipnet.IP.String())
			}
		}
	}
	return ips
}

func main() {
	cfg := config.Load()

	log.Printf("==================================================")
	log.Printf("🐍 Snake Slither Pro Binary Server (Food-Only Mode)")
	log.Printf("🚀 Port: %s | TickRate: %d TPS | Arena: %.0fx%.0f", cfg.Port, cfg.TickRate, cfg.WorldWidth, cfg.WorldHeight)
	log.Printf("==================================================")

	var netManager *network.Manager

	room := game.NewRoom("main-room", cfg, func(state *game.WorldState) {
		// World location broadcasting is paused for clean, step-by-step binary food testing
		// netManager.BroadcastWorldState(state)
	})

	netManager = network.NewManager(room)

	// Start authoritative game loop
	room.StartGameLoop()
	log.Printf("⚡ Authoritative Game Loop running at %d TPS", cfg.TickRate)

	mux := http.NewServeMux()

	// 1. WebSocket Game Endpoint
	mux.HandleFunc("/ws", netManager.HandleWS)

	// 2. Real-Time Telemetry & Multi-Terminal Dashboard
	mux.HandleFunc("/dashboard", monitor.HandleDashboard)
	mux.HandleFunc("/monitor", monitor.HandleDashboard)

	// 3. Authenticated Monitor WebSocket Stream (?token=12345678)
	mux.HandleFunc("/ws/monitor", monitor.DefaultHub.HandleMonitorWS)

	// 4. Action Endpoint for Dashboard
	mux.HandleFunc("/api/monitor/action", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		action := r.URL.Query().Get("action")
		if action == "spawn_foods" {
			countStr := r.URL.Query().Get("count")
			count, _ := strconv.Atoi(countStr)
			if count <= 0 {
				count = 50
			}
			total := room.SpawnCustomFoods(count)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":      "ok",
				"spawned":     count,
				"total_foods": total,
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "unknown_action"})
	})

	// 5. Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"ok","timestamp":%d}`, time.Now().UnixMilli())
	})

	// 6. Game Info endpoint
	mux.HandleFunc("/api/info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		info := map[string]interface{}{
			"status":                "online",
			"active_game_devices":   netManager.GetActiveClientCount(),
			"active_admin_monitors": monitor.DefaultHub.GetSubscriberCount(),
			"world_width":           cfg.WorldWidth,
			"world_height":          cfg.WorldHeight,
			"tick_rate":             cfg.TickRate,
			"timestamp":             time.Now().UnixMilli(),
		}
		json.NewEncoder(w).Encode(info)
	})

	// 7. Active Foods on Arena Endpoint
	mux.HandleFunc("/api/food/active", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		foods := room.GetAllFoodsDTO()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"count": len(foods),
			"foods": foods,
		})
	})

	loggingHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws" && r.URL.Path != "/ws/monitor" {
			monitor.DefaultHub.Emit(monitor.ChanNetwork, "info", "🌐 [HTTP %s] %s (Client: %s)", r.Method, r.URL.Path, r.RemoteAddr)
		}
		mux.ServeHTTP(w, r)
	})

	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: loggingHandler,
	}

	localIPs := getLocalIPs()

	go func() {
		log.Printf("🟢 Server started successfully on port :%s", cfg.Port)
		log.Printf("📊 Real-Time Server Monitor: http://localhost:%s/dashboard", cfg.Port)
		log.Printf("🔌 WebSocket Game Endpoint:  ws://localhost:%s/ws", cfg.Port)
		for _, ip := range localIPs {
			log.Printf("📱 Mobile WebSocket:        ws://%s:%s/ws", ip, cfg.Port)
			log.Printf("📊 Remote Monitor:          http://%s:%s/dashboard", ip, cfg.Port)
		}
		log.Printf("==================================================")

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("❌ Server listen error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	log.Println("🛑 Shutting down server gracefully...")

	room.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("❌ Server forced to shutdown: %v", err)
	}

	log.Println("✅ Server stopped successfully.")
}
