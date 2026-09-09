package main

import (
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	var (
		serverAddr = flag.String("addr", "localhost:8080", "http service address")
		clients    = flag.Int("clients", 10, "number of concurrent websocket clients")
		delayMs    = flag.Int("delay", 5, "delay in milliseconds between messages per client")
	)
	flag.Parse()

	u := url.URL{Scheme: "ws", Host: *serverAddr, Path: "/ws"}
	fmt.Printf("🔥 Starting Go CLI Stress Runner to %s with %d clients, %d ms delay...\n", u.String(), *clients, *delayMs)

	var totalSent uint64
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	for i := 0; i < *clients; i++ {
		clientID := i + 1
		go func(id int) {
			c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
			if err != nil {
				log.Printf("[Client %d] Dial error: %v", id, err)
				return
			}
			defer c.Close()

			ticker := time.NewTicker(time.Duration(*delayMs) * time.Millisecond)
			defer ticker.Stop()

			for {
				select {
				case t := <-ticker.C:
					msg := fmt.Sprintf(`{"client":%d,"seq":%d,"time":"%s"}`, id, atomic.AddUint64(&totalSent, 1), t.Format(time.RFC3339Nano))
					err := c.WriteMessage(websocket.TextMessage, []byte(msg))
					if err != nil {
						return
					}
				}
			}
		}(clientID)
	}

	// Stats reporter
	ticker := time.NewTicker(1 * time.Second)
	var lastCount uint64
	for {
		select {
		case <-ticker.C:
			current := atomic.LoadUint64(&totalSent)
			speed := current - lastCount
			lastCount = current
			fmt.Printf("⚡ [Stress Stats] Total Sent: %10d msgs | Current Speed: %6d msg/sec\n", current, speed)
		case <-interrupt:
			fmt.Printf("\n🛑 Interrupted. Total messages sent: %d\n", atomic.LoadUint64(&totalSent))
			return
		}
	}
}
