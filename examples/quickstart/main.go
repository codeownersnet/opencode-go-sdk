// Example usage of the opencode Go SDK.
//
// Run against a local opencode server:
//
//	opencode serve &
//	go run examples/quickstart/main.go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	opencode "github.com/codeownersnet/opencode-go-sdk"
	"github.com/codeownersnet/opencode-go-sdk/sse"
)

func main() {
	server := os.Getenv("OPENCODE_SERVER")
	if server == "" {
		server = "http://localhost:4096"
	}

	ctx := context.Background()
	client, err := opencode.NewClient(server)
	if err != nil {
		log.Fatalf("create client: %v", err)
	}

	// 1. Health check.
	healthResp, err := client.GlobalHealth(ctx)
	if err != nil {
		log.Fatalf("health check: %v", err)
	}
	defer healthResp.Body.Close()
	fmt.Printf("Server healthy: %s\n", healthResp.Status)

	// 2. Subscribe to global events (SSE).
	stream, err := sse.SubscribeGlobalEvents(ctx, client)
	if err != nil {
		log.Fatalf("subscribe: %v", err)
	}
	defer stream.Close()

	// 3. Create a session.
	createResp, err := client.SessionCreate(ctx, nil, opencode.SessionCreateJSONRequestBody{
		Title: opencode.Ptr("SDK example"),
	})
	if err != nil {
		log.Fatalf("create session: %v", err)
	}
	defer createResp.Body.Close()

	// 4. Print events as they arrive.
	fmt.Println("Listening for events (Ctrl+C to stop)...")
	for stream.Next() {
		ev := stream.Event()
		eventType := sse.GlobalEventType(ev)
		fmt.Printf("  event: %s\n", eventType)

		// Example: handle session.updated events with typed access.
		if eventType == "session.updated" {
			su, err := sse.GlobalEventAs[opencode.GlobalEventSessionUpdated](ev)
			if err != nil {
				log.Printf("    decode error: %v", err)
			} else {
				fmt.Printf("    sessionID: %s\n", su.Properties.SessionID)
			}
		}
	}
	if err := stream.Err(); err != nil {
		log.Printf("stream error: %v", err)
	}
}
