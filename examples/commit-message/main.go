// Example: generating a Conventional Commit message from a git diff.
//
// Fetches the working-tree git diff via the VCS endpoints, then creates
// a session and asks the AI to summarise the diff into a Conventional
// Commit message. Demonstrates composing non-session endpoints with
// session prompts and decoding the Part union from the typed response.
//
// Run against a local opencode server:
//
//	opencode serve &
//	go run examples/commit-message/main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	opencode "github.com/codeownersnet/opencode-go-sdk"
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
	client, err := opencode.NewClientWithResponses(server)
	if err != nil {
		log.Fatalf("create client: %v", err)
	}

	// 1. Get the working-tree status (list of changed files).
	statusResp, err := client.VcsStatusWithResponse(ctx, nil)
	if err != nil {
		log.Fatalf("vcs status: %v", err)
	}
	if statusResp.JSON200 == nil || len(*statusResp.JSON200) == 0 {
		fmt.Println("No uncommitted changes found.")
		return
	}
	fmt.Println("Changed files:")
	for _, f := range *statusResp.JSON200 {
		fmt.Printf("  %s (+%.0f -%.0f)\n", f.File, f.Additions, f.Deletions)
	}

	// 2. Get the unified diff (mode "git" = standard git diff).
	diffResp, err := client.VcsDiffWithResponse(ctx, &opencode.VcsDiffParams{
		Mode: opencode.Git,
	})
	if err != nil {
		log.Fatalf("vcs diff: %v", err)
	}
	if diffResp.JSON200 == nil {
		log.Fatalf("vcs diff: unexpected status %s", diffResp.Status())
	}

	var diff strings.Builder
	for _, d := range *diffResp.JSON200 {
		fmt.Fprintf(&diff, "--- %s\n", d.File)
		if d.Patch != nil {
			fmt.Fprintf(&diff, "%s\n", *d.Patch)
		}
	}
	if diff.Len() == 0 {
		fmt.Println("Diff is empty — nothing to commit.")
		return
	}

	// 3. Create a session and ask for a commit message.
	createResp, err := client.SessionCreateWithResponse(ctx, nil, opencode.SessionCreateJSONRequestBody{
		Title: opencode.Ptr("Commit message generator"),
		Model: &struct {
			Id         string  `json:"id"`
			ProviderID string  `json:"providerID"`
			Variant    *string `json:"variant,omitempty"`
		}{Id: model, ProviderID: provider},
	})
	if err != nil {
		log.Fatalf("create session: %v", err)
	}
	if createResp.JSON200 == nil {
		log.Fatalf("create session: unexpected status %s", createResp.Status())
	}
	sessionID := createResp.JSON200.Id

	prompt := fmt.Sprintf(
		"Given the following git diff, generate a Conventional Commit message. "+
			"Reply with ONLY the commit message (type + colon + space + description), nothing else.\n\n%s",
		diff.String(),
	)

	promptResp, err := client.SessionPromptWithResponse(ctx, sessionID, nil,
		opencode.SessionPromptJSONRequestBody(opencode.TextPromptBody(prompt)),
	)
	if err != nil {
		log.Fatalf("prompt: %v", err)
	}
	if promptResp.JSON200 == nil {
		log.Fatalf("prompt: unexpected status %s", promptResp.Status())
	}

	// 4. Extract text from the assistant's response parts.
	var commitMsg strings.Builder
	for _, part := range promptResp.JSON200.Parts {
		if text, ok := partAsText(part); ok {
			commitMsg.WriteString(text)
		}
	}
	fmt.Printf("\nSuggested commit message:\n  %s\n", strings.TrimSpace(commitMsg.String()))
}

// partAsText extracts the text content from a Part union if it is a text
// part. Since Part is an opaque union (struct { union json.RawMessage }),
// we marshal it to extract the "type" discriminator, then unmarshal into
// the concrete TextPart type.
func partAsText(p opencode.Part) (string, bool) {
	data, err := json.Marshal(p)
	if err != nil {
		return "", false
	}
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return "", false
	}
	if probe.Type != string(opencode.TextPartTypeText) {
		return "", false
	}
	var tp opencode.TextPart
	if err := json.Unmarshal(data, &tp); err != nil {
		return "", false
	}
	return tp.Text, true
}
