package network

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"snake_game_server/internal/config"
	"snake_game_server/internal/game"
)

func TestStartGameMessageFlow(t *testing.T) {
	cfg := &config.Config{
		WorldWidth:  5000,
		WorldHeight: 5000,
		TickRate:    30,
	}
	room := game.NewRoom("test-room-start", cfg, nil)
	netMgr := NewManager(room)

	server := httptest.NewServer(http.HandlerFunc(netMgr.HandleWS))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// 1. Connect WebSocket
	conn, _, err := websocket.DefaultDialer.Dial(wsURL+"?id=test_p1", nil)
	if err != nil {
		t.Fatalf("Failed to dial WebSocket: %v", err)
	}
	defer conn.Close()

	// 2. Read welcome message
	var welcomeMsg BaseMessage
	err = conn.ReadJSON(&welcomeMsg)
	if err != nil {
		t.Fatalf("Failed to read welcome message: %v", err)
	}
	if welcomeMsg.Type != "welcome" {
		t.Errorf("Expected message type 'welcome', got '%s'", welcomeMsg.Type)
	}

	// Verify player is NOT yet in the room before start call
	time.Sleep(20 * time.Millisecond)
	if _, ok := room.GetPlayerSync("test_p1"); ok {
		t.Errorf("Player should not be in room before sending start command")
	}

	// 3. Send Start Game call
	startMsg := map[string]interface{}{
		"type": "start",
		"payload": map[string]interface{}{
			"start":   true,
			"name":    "ProGamer",
			"skin_id": 2,
		},
	}
	if err := conn.WriteJSON(startMsg); err != nil {
		t.Fatalf("Failed to send start message: %v", err)
	}

	// 4. Read started message
	var startedMsg struct {
		Type    string                 `json:"type"`
		Payload map[string]interface{} `json:"payload"`
	}
	err = conn.ReadJSON(&startedMsg)
	if err != nil {
		t.Fatalf("Failed to read started response: %v", err)
	}
	if startedMsg.Type != "started" {
		t.Errorf("Expected message type 'started', got '%s'", startedMsg.Type)
	}
	if startedMsg.Payload["start"] != true {
		t.Errorf("Expected start: true in payload, got %v", startedMsg.Payload["start"])
	}
	if startedMsg.Payload["name"] != "ProGamer" {
		t.Errorf("Expected name 'ProGamer', got %v", startedMsg.Payload["name"])
	}

	// 5. Verify player is now active in room
	if pSync, ok := room.GetPlayerSync("test_p1"); !ok || !pSync.IsAlive {
		t.Errorf("Player should now be alive in room after start command")
	}
}

func TestCompactStartCommand(t *testing.T) {
	cfg := &config.Config{
		WorldWidth:  5000,
		WorldHeight: 5000,
		TickRate:    30,
	}
	room := game.NewRoom("test-room-compact", cfg, nil)
	netMgr := NewManager(room)

	server := httptest.NewServer(http.HandlerFunc(netMgr.HandleWS))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	conn, _, err := websocket.DefaultDialer.Dial(wsURL+"?id=test_p2", nil)
	if err != nil {
		t.Fatalf("Failed to dial WebSocket: %v", err)
	}
	defer conn.Close()

	// Read welcome
	var welcome BaseMessage
	conn.ReadJSON(&welcome)

	// Send single line compact JSON: {"type":"start","start":true,"name":"QuickPlayer"}
	rawMsg := []byte(`{"type":"start","start":true,"name":"QuickPlayer"}`)
	if err := conn.WriteMessage(websocket.TextMessage, rawMsg); err != nil {
		t.Fatalf("Failed to send compact start: %v", err)
	}

	var resp struct {
		Type    string                 `json:"type"`
		Payload map[string]interface{} `json:"payload"`
	}
	if err := conn.ReadJSON(&resp); err != nil {
		t.Fatalf("Failed to read start response: %v", err)
	}

	if resp.Type != "started" {
		t.Errorf("Expected 'started', got '%s'", resp.Type)
	}
	if resp.Payload["name"] != "QuickPlayer" {
		t.Errorf("Expected 'QuickPlayer', got %v", resp.Payload["name"])
	}
}

func TestTokenReconnectionLiveSnake(t *testing.T) {
	cfg := &config.Config{
		WorldWidth:  5000,
		WorldHeight: 5000,
		TickRate:    30,
	}
	room := game.NewRoom("test-room-token-reconnect", cfg, nil)
	netMgr := NewManager(room)

	server := httptest.NewServer(http.HandlerFunc(netMgr.HandleWS))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// 1. First connection: Start game and get token
	conn1, _, err := websocket.DefaultDialer.Dial(wsURL+"?id=token_user_1", nil)
	if err != nil {
		t.Fatalf("Failed to dial WS: %v", err)
	}

	var welcome BaseMessage
	conn1.ReadJSON(&welcome)

	// Send start
	conn1.WriteJSON(map[string]interface{}{
		"type": "start",
		"payload": map[string]interface{}{
			"start": true,
			"name":  "TokenHero",
		},
	})

	var startedMsg struct {
		Type    string                 `json:"type"`
		Payload map[string]interface{} `json:"payload"`
	}
	conn1.ReadJSON(&startedMsg)

	token, ok := startedMsg.Payload["token"].(string)
	if !ok || token == "" {
		t.Fatalf("Expected valid session token in start payload, got %v", startedMsg.Payload["token"])
	}

	// 2. Simulate disconnect
	conn1.Close()
	time.Sleep(50 * time.Millisecond)

	// Verify snake is still alive in room
	pSync, inRoom := room.GetPlayerSync("token_user_1")
	if !inRoom || !pSync.IsAlive {
		t.Fatalf("Expected snake to stay alive in arena after disconnect")
	}

	// 3. Reconnect with token via query param
	conn2, _, err := websocket.DefaultDialer.Dial(wsURL+"?token="+token, nil)
	if err != nil {
		t.Fatalf("Failed to dial reconnect WS: %v", err)
	}
	defer conn2.Close()

	var reconnectMsg struct {
		Type    string                 `json:"type"`
		Payload map[string]interface{} `json:"payload"`
	}
	if err := conn2.ReadJSON(&reconnectMsg); err != nil {
		t.Fatalf("Failed to read reconnect response: %v", err)
	}

	if reconnectMsg.Type != "reconnect_success" {
		t.Errorf("Expected 'reconnect_success', got '%s'", reconnectMsg.Type)
	}
	if reconnectMsg.Payload["token"] != token {
		t.Errorf("Expected token '%s', got %v", token, reconnectMsg.Payload["token"])
	}
	if reconnectMsg.Payload["alive"] != true {
		t.Errorf("Expected alive: true in reconnect payload")
	}
}

func TestTokenReconnectionDeadSnake(t *testing.T) {
	cfg := &config.Config{
		WorldWidth:  5000,
		WorldHeight: 5000,
		TickRate:    30,
	}
	room := game.NewRoom("test-room-token-dead", cfg, nil)
	netMgr := NewManager(room)

	server := httptest.NewServer(http.HandlerFunc(netMgr.HandleWS))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// 1. Connect and start
	conn1, _, err := websocket.DefaultDialer.Dial(wsURL+"?id=token_user_dead", nil)
	if err != nil {
		t.Fatalf("Failed to dial WS: %v", err)
	}

	var welcome BaseMessage
	conn1.ReadJSON(&welcome)

	conn1.WriteJSON(map[string]interface{}{
		"type": "start",
		"payload": map[string]interface{}{
			"start": true,
			"name":  "DoomedSnake",
		},
	})

	var startedMsg struct {
		Type    string                 `json:"type"`
		Payload map[string]interface{} `json:"payload"`
	}
	conn1.ReadJSON(&startedMsg)
	token := startedMsg.Payload["token"].(string)

	// 2. Disconnect and kill player on server
	conn1.Close()
	time.Sleep(30 * time.Millisecond)

	room.RecordPlayerDeath("token_user_dead", "hit_obstacle_while_offline")

	// 3. Try to reconnect using the dead snake's token
	conn2, _, err := websocket.DefaultDialer.Dial(wsURL+"?token="+token, nil)
	if err != nil {
		t.Fatalf("Failed to dial reconnect WS: %v", err)
	}
	defer conn2.Close()

	var gameOverMsg struct {
		Type    string                 `json:"type"`
		Payload map[string]interface{} `json:"payload"`
	}
	if err := conn2.ReadJSON(&gameOverMsg); err != nil {
		t.Fatalf("Failed to read game over response: %v", err)
	}

	if gameOverMsg.Type != "game_over" {
		t.Errorf("Expected 'game_over' response for dead snake token, got '%s'", gameOverMsg.Type)
	}
	if gameOverMsg.Payload["status"] != "dead" {
		t.Errorf("Expected status 'dead', got %v", gameOverMsg.Payload["status"])
	}
	if gameOverMsg.Payload["reason"] != "hit_obstacle_while_offline" {
		t.Errorf("Expected reason 'hit_obstacle_while_offline', got %v", gameOverMsg.Payload["reason"])
	}
}

func TestConcurrentSocketReconnectDeadlockPrevention(t *testing.T) {
	cfg := &config.Config{
		WorldWidth:  5000,
		WorldHeight: 5000,
		TickRate:    30,
	}
	room := game.NewRoom("test-room-deadlock-prevention", cfg, nil)
	netMgr := NewManager(room)

	server := httptest.NewServer(http.HandlerFunc(netMgr.HandleWS))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// 1. First connection
	conn1, _, err := websocket.DefaultDialer.Dial(wsURL+"?id=same_user_1", nil)
	if err != nil {
		t.Fatalf("Failed to dial WS 1: %v", err)
	}

	var welcome1 BaseMessage
	if err := conn1.ReadJSON(&welcome1); err != nil {
		t.Fatalf("Failed to read welcome from conn1: %v", err)
	}

	// 2. Start game on conn1
	conn1.WriteJSON(map[string]interface{}{
		"type": "start",
		"payload": map[string]interface{}{
			"start": true,
			"name":  "MultiConnUser",
		},
	})

	var startResp struct {
		Type    string                 `json:"type"`
		Payload map[string]interface{} `json:"payload"`
	}
	conn1.ReadJSON(&startResp)
	token := startResp.Payload["token"].(string)

	// 3. Immediately connect a second socket with the SAME ID while conn1 is STILL OPEN!
	// This specifically exercises oldClient.Close() invocation within HandleWS
	conn2, _, err := websocket.DefaultDialer.Dial(wsURL+"?id=same_user_1", nil)
	if err != nil {
		t.Fatalf("Failed to dial WS 2 with same ID: %v", err)
	}
	defer conn2.Close()

	var welcome2 BaseMessage
	if err := conn2.ReadJSON(&welcome2); err != nil {
		t.Fatalf("Failed to read welcome from conn2: %v", err)
	}

	// 4. Immediately reconnect using token with a third socket while conn2 is STILL OPEN!
	conn3, _, err := websocket.DefaultDialer.Dial(wsURL+"?token="+token, nil)
	if err != nil {
		t.Fatalf("Failed to dial WS 3 with token: %v", err)
	}
	defer conn3.Close()

	var recMsg struct {
		Type    string                 `json:"type"`
		Payload map[string]interface{} `json:"payload"`
	}
	if err := conn3.ReadJSON(&recMsg); err != nil {
		t.Fatalf("Failed to read reconnect response from conn3: %v", err)
	}
	if recMsg.Type != "reconnect_success" {
		t.Errorf("Expected 'reconnect_success', got '%s'", recMsg.Type)
	}

	// 5. Ensure multiple sequential connects and disconnects work cleanly without hangs
	for i := 0; i < 5; i++ {
		tempConn, _, err := websocket.DefaultDialer.Dial(wsURL+"?id=same_user_1", nil)
		if err != nil {
			t.Fatalf("Failed iteration %d dial: %v", i, err)
		}
		var tempWelcome BaseMessage
		_ = tempConn.ReadJSON(&tempWelcome)
		tempConn.Close()
	}
}

