// Example: injecting custom AI agents via ConfigUpdate.
//
// Demonstrates the full ConfigUpdate -> agent-by-name round-trip: GET the
// current server config, merge in three custom agents (security, performance,
// code quality) under Config.Agent.AdditionalProperties, PATCH the config
// back, then create a session that references one of the custom agents by name
// and stream its response.
//
// The PATCH updates the server's in-memory config — the custom agent's prompt
// and model are used by the session even though GET /agent may not list it.
// This is the pattern that lets prompts live in agent configs on the server
// rather than being embedded in every SessionPromptAsync call.
//
// Caveat: this example mutates the server's config (PATCH /config). It merges
// additively (GET -> merge -> PATCH) so existing config is preserved, but it
// does not restore the original config. The in-memory config does not persist
// across server restarts.
//
// Run against a local opencode server:
//
//	opencode serve &
//	go run examples/custom-agents/main.go
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

const securityPrompt = `You are the SECURITY reviewer. Review only the code
you are given. Flag injection vulnerabilities, auth bypasses, hardcoded
secrets, and insecure crypto. Do not flag theoretical risks or issues in
unchanged code. Be concise.`

const performancePrompt = `You are the PERFORMANCE reviewer. Review only the
code you are given. Flag N+1 queries, O(n^2) over large sets, unbounded
allocations, and synchronous I/O on hot paths. Do not flag
micro-optimizations. Be concise.`

const codeQualityPrompt = `You are the CODE QUALITY reviewer. Review only the
code you are given. Flag logic errors, leaked resources, incorrect error
handling, and broken concurrency. Do not flag naming or style. Be concise.`

// customAgents is the roster injected into the server config.
var customAgents = []struct {
	key         string
	description string
	prompt      string
}{
	{"security_reviewer", "Reviews diffs for security vulnerabilities.", securityPrompt},
	{"performance_reviewer", "Reviews diffs for performance regressions.", performancePrompt},
	{"code_quality_reviewer", "Reviews diffs for logic errors and resource leaks.", codeQualityPrompt},
}

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

	// 1. GET the current config so we can merge additively.
	cfg, err := getConfig(ctx, client)
	if err != nil {
		log.Fatalf("get config: %v", err)
	}
	fmt.Println("Current config agents:")
	printAgents(cfg)

	// 2. Merge in the custom agents (additive — existing config preserved).
	for _, a := range customAgents {
		mergeAgent(cfg, a.key, a.prompt, a.description)
	}

	// 4. PATCH the merged config back to the server. The agents are now
	//    available in-memory for session creation.
	if err := updateConfig(ctx, client, cfg); err != nil {
		log.Fatalf("update config: %v", err)
	}
	fmt.Println("\nCustom agents injected:")
	for _, a := range customAgents {
		fmt.Printf("  %s\n", a.key)
	}

	// 3. Subscribe to per-instance events after the PATCH so we receive
	//    events for the session we're about to create.
	stream, err := sse.SubscribeEvents(ctx, client, nil)
	if err != nil {
		log.Fatalf("subscribe: %v", err)
	}
	defer stream.Close()

	// 4. Create a session referencing a custom agent by name. The server
	//    uses the agent's prompt and model from the patched config.
	const agentName = "code_quality_reviewer"
	resp, err := client.SessionCreate(ctx, nil, opencode.SessionCreateJSONRequestBody{
		Title: opencode.Ptr("Custom agent demo"),
		Agent: opencode.Ptr(agentName),
		Model: &struct {
			Id         string  `json:"id"`
			ProviderID string  `json:"providerID"`
			Variant    *string `json:"variant,omitempty"`
		}{Id: "big-pickle", ProviderID: "opencode"},
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
	fmt.Printf("\nSession: %s (agent: %s)\n\n", sessionID, agentName)

	// 6. Send an async prompt — the system prompt lives in the agent config.
	//    A simple prompt keeps the example focused on the
	//    ConfigUpdate -> agent-by-name round-trip.
	promptResp, err := client.SessionPromptAsync(ctx, sessionID, nil,
		opencode.SessionPromptAsyncJSONRequestBody(
			opencode.TextPromptAsyncBody(
				"Say hello in one sentence.",
			),
		),
	)
	if err != nil {
		log.Fatalf("prompt async: %v", err)
	}
	defer promptResp.Body.Close()

	// 7. Stream the response until the session goes idle.
	for stream.Next() {
		ev := stream.Event()
		switch sse.EventType(ev) {

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
				fmt.Println("\n\n— done —")
				return
			}

		case "session.error":
			se, err := sse.EventAs[opencode.EventSessionError](ev)
			if err != nil {
				continue
			}
			// EventSessionError.Properties.SessionID is *string.
			if se.Properties.SessionID == nil {
				continue
			}
			if *se.Properties.SessionID == sessionID {
				log.Printf("session error")
				return
			}
		}
	}
	if err := stream.Err(); err != nil {
		log.Printf("stream error: %v", err)
	}
}

// getConfig fetches the current server config.
func getConfig(ctx context.Context, client *opencode.Client) (*opencode.Config, error) {
	resp, err := client.ConfigGet(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var cfg opencode.Config
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// updateConfig PATCHes the merged config back to the server.
func updateConfig(ctx context.Context, client *opencode.Client, cfg *opencode.Config) error {
	resp, err := client.ConfigUpdate(ctx, nil,
		opencode.ConfigUpdateJSONRequestBody(*cfg),
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("config update: unexpected status %s", resp.Status)
	}
	return nil
}

// mergeAgent additively sets a custom agent entry in Config.Agent.
// The named built-in agents (build, plan, general, ...) are untouched.
func mergeAgent(cfg *opencode.Config, key, prompt, description string) {
	if cfg.Agent == nil {
		cfg.Agent = &opencode.Config_Agent{}
	}
	if cfg.Agent.AdditionalProperties == nil {
		cfg.Agent.AdditionalProperties = make(map[string]opencode.AgentConfig)
	}
	cfg.Agent.AdditionalProperties[key] = opencode.AgentConfig{
		Prompt:      opencode.Ptr(prompt),
		Mode:        opencode.Ptr(opencode.AgentConfigModePrimary),
		Description: opencode.Ptr(description),
		Model:       opencode.Ptr("big-pickle"),
	}
}

// printAgents lists the agent names present in a Config.
func printAgents(cfg *opencode.Config) {
	if cfg.Agent == nil {
		fmt.Println("  (none)")
		return
	}
	// Named built-in agents.
	for _, name := range []string{"build", "compaction", "explore", "general", "plan", "summary", "title"} {
		var present bool
		switch name {
		case "build":
			present = cfg.Agent.Build != nil
		case "compaction":
			present = cfg.Agent.Compaction != nil
		case "explore":
			present = cfg.Agent.Explore != nil
		case "general":
			present = cfg.Agent.General != nil
		case "plan":
			present = cfg.Agent.Plan != nil
		case "summary":
			present = cfg.Agent.Summary != nil
		case "title":
			present = cfg.Agent.Title != nil
		}
		if present {
			fmt.Printf("  %s (built-in)\n", name)
		}
	}
	for name := range cfg.Agent.AdditionalProperties {
		fmt.Printf("  %s\n", name)
	}
}
