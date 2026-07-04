// Example: parallel code scaffolding with concurrent sessions.
//
// Spins up three sessions in parallel, each generating a different code
// artifact (a CRUD handler, its tests, and a database migration) via
// synchronous prompts. Demonstrates concurrent use of the typed
// ClientWithResponses with sync.WaitGroup and errors.Join.
//
// Run against a local opencode server:
//
//	opencode serve &
//	go run examples/parallel-scaffold/main.go
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"

	opencode "github.com/codeownersnet/opencode-go-sdk"
)

// scaffoldTask represents a single code-generation task.
type scaffoldTask struct {
	title  string
	prompt string
}

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

	tasks := []scaffoldTask{
		{
			title: "CRUD handler",
			prompt: "Generate a Go HTTP handler for a CRUD API managing Task entities " +
				"(fields: ID, Title, Done). Include create, read, update, and delete endpoints.",
		},
		{
			title: "Handler tests",
			prompt: "Generate a Go test file with table-driven tests for a CRUD HTTP handler " +
				"that manages Task entities. Cover success and error cases.",
		},
		{
			title: "Database migration",
			prompt: "Generate a SQL migration to create a tasks table with columns: " +
				"id (uuid), title (text), done (boolean), created_at, updated_at.",
		},
	}

	// Run all scaffold tasks concurrently.
	var wg sync.WaitGroup
	errs := make([]error, len(tasks))
	ids := make([]string, len(tasks))

	for i, task := range tasks {
		wg.Add(1)
		go func(i int, task scaffoldTask) {
			defer wg.Done()
			ids[i], errs[i] = scaffold(ctx, client, task)
		}(i, task)
	}
	wg.Wait()

	if err := errors.Join(errs...); err != nil {
		log.Fatalf("scaffold failed: %v", err)
	}

	fmt.Println("\nGenerated sessions:")
	for i, task := range tasks {
		fmt.Printf("  %d. %s -> session %s\n", i+1, task.title, ids[i])
	}

	// List all sessions to confirm they were created.
	listResp, err := client.SessionListWithResponse(ctx, &opencode.SessionListParams{
		Limit: opencode.Ptr(float32(10)),
	})
	if err != nil {
		log.Fatalf("list sessions: %v", err)
	}
	if listResp.JSON200 != nil {
		fmt.Printf("\nTotal sessions on server: %d\n", len(*listResp.JSON200))
	}
}

// scaffold creates a session and sends a synchronous prompt, returning the
// session ID or an error.
func scaffold(ctx context.Context, client *opencode.ClientWithResponses, task scaffoldTask) (string, error) {
	createResp, err := client.SessionCreateWithResponse(ctx, nil, opencode.SessionCreateJSONRequestBody{
		Title: opencode.Ptr(task.title),
	})
	if err != nil {
		return "", fmt.Errorf("create session %q: %w", task.title, err)
	}
	if createResp.JSON200 == nil {
		return "", fmt.Errorf("create session %q: unexpected status %s", task.title, createResp.Status())
	}
	sessionID := createResp.JSON200.Id

	promptResp, err := client.SessionPromptWithResponse(ctx, sessionID, nil,
		opencode.SessionPromptJSONRequestBody(opencode.TextPromptBody(task.prompt)),
	)
	if err != nil {
		return sessionID, fmt.Errorf("prompt session %q: %w", task.title, err)
	}
	if promptResp.JSON200 == nil {
		return sessionID, fmt.Errorf("prompt session %q: unexpected status %s", task.title, promptResp.Status())
	}

	fmt.Printf("done: %s (session %s)\n", task.title, sessionID)
	return sessionID, nil
}
