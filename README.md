# snake_game_server

A high-performance Go (Golang) backend server for the Snake Game.

## Features
- **HTTP Server**: Built with Go's robust `net/http` package.
- **REST API Endpoints**:
  - `GET /` — Server status landing page & dashboard
  - `GET /api/health` — Health check & uptime monitor
- **Fast Execution**: Precompiled binary support and fast startup.

## How to Run

### Option 1: Run with Go
```bash
go run .
```

### Option 2: Build and Run Binary
```bash
go build -o server.exe .
./server.exe
```

### Option 3: Double click `run.bat` (Windows)
Double-click `run.bat` to launch the server instantly on port `8080`.
