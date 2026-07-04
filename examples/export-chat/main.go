// Example: exporting a past conversation to a markdown file.
//
// Lists all sessions, picks the most recent, retrieves its messages,
// decodes the Message and Part unions, and writes a formatted markdown
// transcript to chat-export.md. Demonstrates the read-heavy API surface
// and union decoding for both Message and Part types.
//
// Run against a local opencode server:
//
//	opencode serve &
//	go run examples/export-chat/main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	opencode "github.com/codeownersnet/opencode-go-sdk"
)

func main() {
	server := os.Getenv("OPENCODE_SERVER")
	if server == "" {
		server = "http://localhost:4096"
	}

	ctx := context.Background()
	client, err := opencode.NewClientWithResponses(server)
	if err != nil {
		log.Fatalf("create client: %v", err)
	}

	// 1. List sessions (limited to the 20 most recent).
	listResp, err := client.SessionListWithResponse(ctx, &opencode.SessionListParams{
		Limit: opencode.Ptr(float32(20)),
	})
	if err != nil {
		log.Fatalf("list sessions: %v", err)
	}
	if listResp.JSON200 == nil || len(*listResp.JSON200) == 0 {
		fmt.Println("No sessions found.")
		return
	}

	// 2. Pick the most recently created session.
	sessions := *listResp.JSON200
	var session opencode.Session
	for _, s := range sessions {
		if s.Time.Created > session.Time.Created {
			session = s
		}
	}
	fmt.Printf("Exporting session: %s (title: %q)\n", session.Id, session.Title)

	// 3. Get detailed session metadata (demonstrates SessionGet).
	getResp, err := client.SessionGetWithResponse(ctx, session.Id, nil)
	if err != nil {
		log.Fatalf("get session: %v", err)
	}
	if getResp.JSON200 == nil {
		log.Fatalf("get session: unexpected status %s", getResp.Status())
	}
	info := getResp.JSON200
	created := time.Unix(int64(info.Time.Created), 0).Format(time.RFC3339)

	// 4. Retrieve all messages in the session.
	msgsResp, err := client.SessionMessagesWithResponse(ctx, session.Id, nil)
	if err != nil {
		log.Fatalf("get messages: %v", err)
	}
	if msgsResp.JSON200 == nil {
		log.Fatalf("get messages: unexpected status %s", msgsResp.Status())
	}
	messages := *msgsResp.JSON200

	// 5. Build the markdown transcript.
	var md strings.Builder
	fmt.Fprintf(&md, "# Session: %s\n\n", info.Title)
	fmt.Fprintf(&md, "- **Session ID**: %s\n", info.Id)
	fmt.Fprintf(&md, "- **Created**: %s\n", created)
	fmt.Fprintf(&md, "- **Messages**: %d\n\n", len(messages))
	fmt.Fprintf(&md, "---\n\n")

	for _, msg := range messages {
		switch messageRole(msg.Info) {
		case "user":
			fmt.Fprintf(&md, "## User\n\n")
		case "assistant":
			fmt.Fprintf(&md, "## Assistant\n\n")
		default:
			fmt.Fprintf(&md, "## Message\n\n")
		}

		for _, part := range msg.Parts {
			renderPart(&md, part)
		}
		fmt.Fprintln(&md)
	}

	// 6. Write to file.
	const outFile = "chat-export.md"
	if err := os.WriteFile(outFile, []byte(md.String()), 0o644); err != nil {
		log.Fatalf("write file: %v", err)
	}
	fmt.Printf("Wrote %s (%d bytes)\n", outFile, md.Len())
}

// messageRole extracts the "role" field from a Message union. Since
// Message is an opaque union (struct { union json.RawMessage }), we
// marshal it to JSON and probe the "role" discriminator.
func messageRole(m opencode.Message) string {
	data, err := json.Marshal(m)
	if err != nil {
		return "unknown"
	}
	var probe struct {
		Role string `json:"role"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return "unknown"
	}
	return probe.Role
}

// renderPart appends a part's content to the markdown builder. Text parts
// are rendered as plain text; tool parts are rendered as code blocks.
func renderPart(md *strings.Builder, p opencode.Part) {
	data, err := json.Marshal(p)
	if err != nil {
		return
	}
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return
	}

	switch probe.Type {
	case string(opencode.TextPartTypeText):
		var tp opencode.TextPart
		if err := json.Unmarshal(data, &tp); err != nil {
			return
		}
		fmt.Fprintf(md, "%s\n\n", tp.Text)

	case string(opencode.Tool):
		var tp opencode.ToolPart
		if err := json.Unmarshal(data, &tp); err != nil {
			return
		}
		fmt.Fprintf(md, "```tool\nname: %s\ncall: %s\n```\n\n", tp.Tool, tp.CallID)
	}
}
