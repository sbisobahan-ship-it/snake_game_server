package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

type ServerStatus struct {
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	Server    string    `json:"server"`
	Timestamp time.Time `json:"timestamp"`
	Uptime    string    `json:"uptime"`
}

var startTime = time.Now()

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()

	// API Health check endpoint
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		status := ServerStatus{
			Status:    "OK",
			Message:   "Snake Game Go Server is healthy and running!",
			Server:    "Go HTTP Engine",
			Timestamp: time.Now(),
			Uptime:    time.Since(startTime).Round(time.Second).String(),
		}
		json.NewEncoder(w).Encode(status)
	})

	// Home / Dashboard endpoint
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Go Server Running - Snake Game Server</title>
    <style>
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body {
            font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
            background: radial-gradient(circle at top, #1e1e38, #0f0f1a);
            color: #ffffff;
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
            padding: 20px;
        }
        .card {
            background: rgba(255, 255, 255, 0.05);
            backdrop-filter: blur(16px);
            border: 1px solid rgba(0, 255, 204, 0.2);
            border-radius: 20px;
            padding: 40px;
            max-width: 600px;
            width: 100%;
            box-shadow: 0 20px 50px rgba(0, 0, 0, 0.5), 0 0 30px rgba(0, 255, 204, 0.1);
            text-align: center;
        }
        .badge {
            display: inline-flex;
            align-items: center;
            gap: 8px;
            background: rgba(0, 255, 204, 0.15);
            color: #00ffcc;
            padding: 6px 16px;
            border-radius: 50px;
            font-size: 14px;
            font-weight: 600;
            margin-bottom: 20px;
            border: 1px solid rgba(0, 255, 204, 0.3);
        }
        .pulse {
            width: 10px;
            height: 10px;
            background: #00ffcc;
            border-radius: 50%;
            box-shadow: 0 0 10px #00ffcc;
            animation: pulse 1.5s infinite;
        }
        @keyframes pulse {
            0%, 100% { opacity: 1; transform: scale(1); }
            50% { opacity: 0.4; transform: scale(1.3); }
        }
        h1 {
            font-size: 28px;
            font-weight: 700;
            margin-bottom: 12px;
            color: #fff;
        }
        p {
            color: #a0a0c0;
            font-size: 16px;
            line-height: 1.6;
            margin-bottom: 24px;
        }
        .endpoints {
            background: rgba(0, 0, 0, 0.3);
            border-radius: 12px;
            padding: 16px;
            text-align: left;
            margin-bottom: 25px;
            font-family: 'Consolas', 'Courier New', monospace;
            font-size: 14px;
        }
        .endpoint-row {
            display: flex;
            justify-content: space-between;
            padding: 8px 0;
            border-bottom: 1px solid rgba(255, 255, 255, 0.05);
        }
        .endpoint-row:last-child { border-bottom: none; }
        .method { color: #00ffcc; font-weight: bold; }
        .path { color: #f0f0ff; }
        .btn {
            display: inline-block;
            background: linear-gradient(135deg, #00ffcc, #0099ff);
            color: #0b0c16;
            text-decoration: none;
            padding: 12px 28px;
            border-radius: 10px;
            font-weight: 700;
            transition: all 0.3s ease;
            box-shadow: 0 4px 15px rgba(0, 255, 204, 0.3);
        }
        .btn:hover {
            transform: translateY(-2px);
            box-shadow: 0 8px 25px rgba(0, 255, 204, 0.5);
        }
    </style>
</head>
<body>
    <div class="card">
        <div class="badge">
            <span class="pulse"></span>
            Server Online & Ready
        </div>
        <h1>🚀 Go Server চলছে সফলভাবে!</h1>
        <p>আপনার <strong>Snake Game Go Server</strong> সফলভাবে স্টার্ট হয়েছে এবং লোকাল পোর্টে রিকোয়েস্ট গ্রহণ করছে।</p>
        
        <div class="endpoints">
            <div class="endpoint-row">
                <span class="method">GET</span>
                <span class="path">/ (Home Dashboard)</span>
            </div>
            <div class="endpoint-row">
                <span class="method">GET</span>
                <span class="path">/api/health (JSON Status)</span>
            </div>
        </div>

        <a href="/api/health" target="_blank" class="btn">Test API Health (/api/health)</a>
    </div>
</body>
</html>`
		w.Write([]byte(html))
	})

	addr := ":" + port
	fmt.Printf("========================================\n")
	fmt.Printf("🚀 Go Server is running on http://localhost:%s\n", port)
	fmt.Printf("📡 Health Check: http://localhost:%s/api/health\n", port)
	fmt.Printf("========================================\n")

	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}
