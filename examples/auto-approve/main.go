// Example: auto-approving tool permissions for hands-off coding.
//
// Sends a prompt that triggers file edits requiring permission, then
// monitors the SSE stream for permission.asked events and auto-responds
// with "always allow". This builds a fully autonomous coding workflow
// where the AI can write files without human interaction.
//
// Run against a local opencode server:
//
//	opencode serve &
//	go run examples/auto-approve/main.go
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
	model := os.Getenv("OPENCODE_MODEL")
	if model == "" {
		model = "big-pickle"
	}
	provider := os.Getenv("OPENCODE_PROVIDER")
	if provider == "" {
		provider = "opencode"
	}

	ctx := context.Background()
	client, err := opencode.NewClient(server)
	if err != nil {
		log.Fatalf("create client: %v", err)
	}

	// 1. Subscribe to per-instance events before sending the prompt.
	stream, err := sse.SubscribeEvents(ctx, client, nil)
	if err != nil {
		log.Fatalf("subscribe: %v", err)
	}
	defer stream.Close()

	// 2. Create a session.
	resp, err := client.SessionCreate(ctx, nil, opencode.SessionCreateJSONRequestBody{
		Title: opencode.Ptr("Auto-approve demo"),
		Model: &struct {
			Id         string  `json:"id"`
			ProviderID string  `json:"providerID"`
			Variant    *string `json:"variant,omitempty"`
		}{Id: model, ProviderID: provider},
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

	// 3. Send an async prompt that will trigger tool calls requiring permission.
	promptResp, err := client.SessionPromptAsync(ctx, sessionID, nil,
		opencode.SessionPromptAsyncJSONRequestBody(
			opencode.TextPromptAsyncBody(
				"Create a new file called hello.txt with the content 'Hello, World!' "+
					"and then read it back to verify.",
			),
		),
	)
	if err != nil {
		log.Fatalf("prompt async: %v", err)
	}
	defer promptResp.Body.Close()

	// 4. Process events — auto-approve any permission requests and print
	//    streaming text as it arrives.
	for stream.Next() {
		ev := stream.Event()
		switch sse.EventType(ev) {

		case "permission.asked":
			pa, err := sse.EventAs[opencode.EventPermissionAsked](ev)
			if err != nil {
				continue
			}
			fmt.Printf("[permission] %s — auto-approving...\n", pa.Properties.Permission)

			// Auto-respond with "always allow" so future calls of the
			// same type don't ask again.
			permResp, err := client.PermissionRespond(
				ctx,
				pa.Properties.SessionID,
				pa.Properties.Id,
				nil,
				opencode.PermissionRespondJSONRequestBody{
					Response: opencode.PermissionRespondJSONBodyResponseAlways,
				},
			)
			if err != nil {
				log.Printf("permission respond: %v", err)
				continue
			}
			permResp.Body.Close()

		case "session.next.text.delta":
			d, err := sse.EventAs[opencode.EventSessionNextTextDelta](ev)
			if err != nil {
				continue
			}
			if d.Properties.SessionID == sessionID {
				fmt.Print(d.Properties.Delta)
			}

		case "message.part.delta":
			d, err := sse.EventAs[opencode.EventMessagePartDelta](ev)
			if err != nil {
				continue
			}
			if d.Properties.SessionID == sessionID && d.Properties.Field == "text" {
				fmt.Print(d.Properties.Delta)
			}

		case "session.next.tool.called":
			tc, err := sse.EventAs[opencode.EventSessionNextToolCalled](ev)
			if err != nil {
				continue
			}
			if tc.Properties.SessionID == sessionID {
				fmt.Printf("\n[tool] %s\n", tc.Properties.Tool)
			}

		case "session.idle":
			si, err := sse.EventAs[opencode.EventSessionIdle](ev)
			if err != nil {
				continue
			}
			if si.Properties.SessionID == sessionID {
				fmt.Println("\n\nDone — session is idle.")
				return
			}

		case "session.error":
			se, err := sse.EventAs[opencode.EventSessionError](ev)
			if err != nil {
				continue
			}
			if se.Properties.SessionID != nil && *se.Properties.SessionID == sessionID {
				log.Printf("session error event received")
				return
			}
		}
	}
	if err := stream.Err(); err != nil {
		log.Printf("stream error: %v", err)
	}
}
