// Example: streaming a code review in real time.
//
// Creates a session, sends an asynchronous prompt asking the AI to review
// code, and prints the response token-by-token as it streams over SSE.
// Tool calls are logged as they happen. Uses the raw Client (required for
// SSE) together with the hand-written sse package for typed event dispatch.
//
// Run against a local opencode server:
//
//	opencode serve &
//	go run examples/streaming-review/main.go
package main

import (
	"context"
	"encoding/json"
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

	// 1. Subscribe to per-instance events (SSE) before creating the session
	//    so we don't miss any early events.
	stream, err := sse.SubscribeEvents(ctx, client, nil)
	if err != nil {
		log.Fatalf("subscribe: %v", err)
	}
	defer stream.Close()

	// 2. Create a session.
	resp, err := client.SessionCreate(ctx, nil, opencode.SessionCreateJSONRequestBody{
		Title: opencode.Ptr("Streaming code review"),
	})
	if err != nil {
		log.Fatalf("create session: %v", err)
	}
	defer resp.Body.Close()

	var session opencode.Session
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		log.Fatalf("decode session: %v", err)
	}
	sessionID := session.Id
	fmt.Printf("Session: %s\n\n", sessionID)

	// 3. Send an asynchronous prompt — the server returns immediately and
	//    the response streams in via SSE events.
	promptResp, err := client.SessionPromptAsync(ctx, sessionID, nil,
		opencode.SessionPromptAsyncJSONRequestBody(
			opencode.TextPromptAsyncBody(
				"Review the main.go file for bugs, style issues, and potential improvements. Be concise.",
			),
		),
	)
	if err != nil {
		log.Fatalf("prompt async: %v", err)
	}
	defer promptResp.Body.Close()

	// 4. Consume the SSE stream until the session goes idle.
	for stream.Next() {
		ev := stream.Event()
		switch sse.EventType(ev) {

		case "session.next.text.delta":
			d, err := sse.EventAs[opencode.EventSessionNextTextDelta](ev)
			if err != nil {
				continue
			}
			fmt.Print(d.Properties.Delta)

		case "session.next.tool.called":
			tc, err := sse.EventAs[opencode.EventSessionNextToolCalled](ev)
			if err != nil {
				continue
			}
			fmt.Printf("\n[tool] calling %s (call %s)\n", tc.Properties.Tool, tc.Properties.CallID)

		case "session.next.tool.success":
			ts, err := sse.EventAs[opencode.EventSessionNextToolSuccess](ev)
			if err != nil {
				continue
			}
			fmt.Printf("\n[tool] call %s succeeded\n", ts.Properties.CallID)

		case "session.next.tool.failed":
			tf, err := sse.EventAs[opencode.EventSessionNextToolFailed](ev)
			if err != nil {
				continue
			}
			fmt.Printf("\n[tool] call %s FAILED\n", tf.Properties.CallID)

		case "session.next.step.ended":
			se, err := sse.EventAs[opencode.EventSessionNextStepEnded](ev)
			if err != nil {
				continue
			}
			fmt.Printf("\n\n— step done — tokens in: %.0f, out: %.0f, finish: %s\n",
				se.Properties.Tokens.Input, se.Properties.Tokens.Output, se.Properties.Finish)

		case "session.idle":
			fmt.Println("\n\nSession is idle — review complete.")
			return

		case "session.error":
			log.Printf("session error event received")
			return
		}
	}
	if err := stream.Err(); err != nil {
		log.Printf("stream error: %v", err)
	}
}
