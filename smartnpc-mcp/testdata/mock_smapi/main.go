// Mock SMAPI WebSocket server for integration testing.
// Listens on :18745/ws, accepts one client, sends a chat_received event after
// 2 seconds, and logs any requests it receives back (e.g. chat_say).
//
// Usage: go run testdata/mock_smapi/main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", handleWS)
	log.Println("mock SMAPI ws server listening on :18745")
	if err := http.ListenAndServe("127.0.0.1:18745", mux); err != nil {
		log.Fatal(err)
	}
}

func handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Println("accept:", err)
		return
	}
	defer conn.CloseNow()
	log.Println("ws client connected")

	ctx := r.Context()

	// Send chat_received event after delay
	go func() {
		time.Sleep(2 * time.Second)
		event := map[string]any{
			"type":      "event",
			"name":      "chat_received",
			"data":      map[string]any{"text": "hello NPC!", "source": "player"},
			"timestamp": time.Now().UnixMilli(),
		}
		b, _ := json.Marshal(event)
		log.Printf(">> sending event: %s", string(b))
		if err := conn.Write(ctx, websocket.MessageText, b); err != nil {
			log.Println("write event:", err)
		}
	}()

	// Read loop
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			log.Println("ws read done:", err)
			return
		}
		log.Printf("<< received: %s", string(data))

		var frame struct {
			Type   string `json:"type"`
			ID     string `json:"id"`
			Action string `json:"action"`
		}
		json.Unmarshal(data, &frame)
		if frame.Type == "request" && frame.ID != "" {
			resp := map[string]any{
				"type": "response",
				"id":   frame.ID,
				"ok":   true,
				"data": map[string]any{},
			}
			if err := wsjson.Write(context.Background(), conn, resp); err != nil {
				log.Println("write response:", err)
			}
			log.Printf(">> sent response for id=%s action=%s", frame.ID, frame.Action)
		}
	}
}

func init() {
	_ = fmt.Sprintf // suppress unused import
}
