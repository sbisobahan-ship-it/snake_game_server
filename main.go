package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"snake_game_server/pkg/game"
	"snake_game_server/pkg/hub"
)

type ServerStatus struct {
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	Engine    string    `json:"engine"`
	Clients   int       `json:"connectedClients"`
	Timestamp time.Time `json:"timestamp"`
	Uptime    string    `json:"uptime"`
}

var startTime = time.Now()

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Initialize 40x30 Grid Real-time Game Engine (matches 800x600 canvas)
	gameEngine := game.NewGameEngine(40, 30)
	gameEngine.Start()
	defer gameEngine.Stop()

	// Initialize WebSocket Hub
	wsHub := hub.NewHub(gameEngine)
	go wsHub.Run()

	mux := http.NewServeMux()

	// WebSocket Endpoint
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		wsHub.ServeWs(w, r)
	})

	// Static Assets (/static/style.css, /static/game.js, etc.)
	fs := http.FileServer(http.Dir("./static"))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))

	// REST API Health Check Endpoint
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		status := ServerStatus{
			Status:    "OK",
			Message:   "Snake Game WebSocket Server is active and accepting connections",
			Engine:    "Go 1.22 + Gorilla WebSocket",
			Clients:   wsHub.PlayerCount(),
			Timestamp: time.Now(),
			Uptime:    time.Since(startTime).Round(time.Second).String(),
		}
		json.NewEncoder(w).Encode(status)
	})

	// Root Handler - Serves the Neon Snake Game UI
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(".", "static", "index.html"))
	})

	addr := ":" + port
	fmt.Printf("====================================================\n")
	fmt.Printf("🐍 Snake Game Go WebSocket Server Online!\n")
	fmt.Printf("🌐 Web Game Client : http://localhost:%s\n", port)
	fmt.Printf("⚡ WebSocket Stream : ws://localhost:%s/ws\n", port)
	fmt.Printf("📡 Health API       : http://localhost:%s/api/health\n", port)
	fmt.Printf("====================================================\n")

	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}
