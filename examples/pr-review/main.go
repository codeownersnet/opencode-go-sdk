// Example: reviewing a pull request with specialised AI agents.
//
// Orchestrates multiple domain-specialist reviewers (security, performance,
// code quality) plus a coordinator judge pass. The PR diff is the current
// branch against the repo's default branch, fetched natively via the SDK's
// GET /vcs/diff?mode=branch endpoint.
//
// The example demonstrates several patterns at once:
//   - Fetching a branch-vs-default diff via the VCS endpoints.
//   - Diff filtering (lock files, minified assets) and risk-tier assessment.
//   - Launching multiple concurrent sessions with async prompts.
//   - Multiplexing a single /event SSE stream across sessions by SessionID.
//   - Auto-approving permission.asked events so reviewers can use tools.
//   - A two-phase state machine (specialists -> coordinator) on one stream.
//
// Run against a local opencode server:
//
//	opencode serve &
//	go run examples/pr-review/main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	opencode "github.com/codeownersnet/opencode-go-sdk"
	"github.com/codeownersnet/opencode-go-sdk/sse"
)

// reviewerSpec is one specialist reviewer's identity + prompt.
// key is a stable id used in the coordinator's <findings reviewer="key"> blocks.
type reviewerSpec struct {
	key    string
	name   string
	prompt string
}

// reviewerState tracks one specialist reviewer's progress on the SSE stream.
type reviewerState struct {
	spec      reviewerSpec
	sessionID string
	findings  strings.Builder
	idle      bool
	errored   bool
}

// reviewerPreamble is shared by every specialist reviewer. The diff is
// appended at the end wrapped in <diff> tags by the caller.
const reviewerPreamble = `Review ONLY the diff below. Use the repo tools (read files, grep) to verify context when needed, but do not modify anything.

## What NOT to Flag (shared)
- Theoretical risks that require unlikely preconditions
- Issues in unchanged code that this PR doesn't affect
- "Consider using library X" style suggestions

## Output format
Return a list of findings, one per block:
SEVERITY: CRITICAL | WARNING | SUGGESTION
FILE: <path>
LINE: <line number or "unknown">
DETAIL: <one concise sentence>
If you find nothing to flag, reply exactly: No issues found.
`

var securitySpec = reviewerSpec{
	key:  "security",
	name: "Security",
	prompt: `You are the SECURITY reviewer for a pull request.
` + reviewerPreamble + `
## What to Flag
- Injection vulnerabilities (SQL, XSS, command, path traversal)
- Authentication/authorisation bypasses in changed code
- Hardcoded secrets, credentials, or API keys
- Insecure cryptographic usage
- Missing input validation on untrusted data at trust boundaries
`,
}

var performanceSpec = reviewerSpec{
	key:  "performance",
	name: "Performance",
	prompt: `You are the PERFORMANCE reviewer for a pull request.
` + reviewerPreamble + `
## What to Flag
- N+1 queries or repeated work inside loops
- O(n^2) or worse over large data sets
- Unbounded allocations or goroutine growth
- Missing indexes on hot database paths
- Synchronous I/O on request hot paths
`,
}

var codeQualitySpec = reviewerSpec{
	key:  "code_quality",
	name: "Code Quality",
	prompt: `You are the CODE QUALITY reviewer for a pull request.
` + reviewerPreamble + `
## What to Flag
- Logic errors (wrong condition, off-by-one, inverted check)
- Leaked resources (unclosed file/conn, forgotten defer)
- Incorrect error handling (swallowed errors, wrong wrap)
- Broken concurrency (data races, missing lock, deadlock)
`,
}

// reviewerSpecs is the full roster, in launch order.
var reviewerSpecs = []reviewerSpec{securitySpec, performanceSpec, codeQualitySpec}

// coordinatorPreamble instructs the judge session on how to consolidate.
const coordinatorPreamble = `You are the review coordinator. Specialist reviewers analyzed this PR.
- Deduplicate findings flagged by multiple reviewers (keep once, in the best-fit section).
- Re-categorize misplaced findings (e.g. a perf issue from code quality -> performance).
- Drop speculative, theoretical, or false-positive findings; if unsure, check the diff.
- Bias toward approval: a single WARNING in an otherwise clean PR is APPROVED_WITH_COMMENTS.

Emit:
VERDICT: APPROVED | APPROVED_WITH_COMMENTS | NEEDS_CHANGES

Then grouped findings under ### CRITICAL, ### WARNING, ### SUGGESTION.
If none, write "No issues — looks good."
`

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

	// 1. Subscribe to per-instance events (SSE) before creating sessions
	//    so we don't miss any early events.
	stream, err := sse.SubscribeEvents(ctx, client, nil)
	if err != nil {
		log.Fatalf("subscribe: %v", err)
	}
	defer stream.Close()

	// 2. VCS info — show which branch we're reviewing against.
	infoResp, err := client.VcsGet(ctx, nil)
	if err != nil {
		log.Fatalf("vcs get: %v", err)
	}
	var info opencode.VcsInfo
	if err := json.NewDecoder(infoResp.Body).Decode(&info); err != nil {
		infoResp.Body.Close()
		log.Fatalf("decode vcs info: %v", err)
	}
	infoResp.Body.Close()

	branch, def := "<unknown>", "<unknown>"
	if info.Branch != nil {
		branch = *info.Branch
	}
	if info.DefaultBranch != nil {
		def = *info.DefaultBranch
	}
	fmt.Printf("Reviewing PR: %s -> %s\n\n", branch, def)

	// 3. PR diff — current branch against the default branch.
	diffResp, err := client.VcsDiff(ctx, &opencode.VcsDiffParams{
		Mode: opencode.Branch,
	})
	if err != nil {
		log.Fatalf("vcs diff: %v", err)
	}
	var diffs []opencode.VcsFileDiff
	if err := json.NewDecoder(diffResp.Body).Decode(&diffs); err != nil {
		diffResp.Body.Close()
		log.Fatalf("decode vcs diff: %v", err)
	}
	diffResp.Body.Close()

	if len(diffs) == 0 {
		fmt.Printf("No PR diff — are you on the default branch %q?\n", def)
		return
	}

	// 4. Filter noise (lock files, minified assets, source maps).
	//    Migrations are exempt: they may be marked generated but still
	//    need review.
	filtered, skipped := filterDiff(diffs)
	fmt.Printf("Changed files: %d (skipped %d noise files)\n", len(filtered), skipped)
	for _, d := range filtered {
		fmt.Printf("  %s (+%.0f -%.0f)\n", d.File, d.Additions, d.Deletions)
	}

	// 5. Risk tier — decide how many reviewers to launch.
	tier := assessRiskTier(filtered)
	specs := specsForTier(tier)
	fmt.Printf("\nRisk tier: %s — launching %d reviewer(s): %s\n\n",
		tier, len(specs), specNames(specs))

	// 6. Build the diff string for the prompts.
	var diff strings.Builder
	for _, d := range filtered {
		fmt.Fprintf(&diff, "--- %s\n", d.File)
		if d.Patch != nil {
			fmt.Fprintf(&diff, "%s\n", *d.Patch)
		}
	}

	// 7. Launch reviewers — each gets its own session + async prompt.
	//    Sequential creation is fine: async prompts return immediately and
	//    events buffer in the SSE stream.
	states := make([]*reviewerState, 0, len(specs))
	bySession := make(map[string]*reviewerState, len(specs))
	for _, spec := range specs {
		id, err := createSession(ctx, client, spec.name+" review")
		if err != nil {
			log.Fatalf("create %s session: %v", spec.name, err)
		}
		fullPrompt := spec.prompt + "\n<diff>\n" + diff.String() + "\n</diff>\n"
		if err := sendAsyncPrompt(ctx, client, id, fullPrompt); err != nil {
			log.Fatalf("prompt async %s: %v", spec.name, err)
		}
		st := &reviewerState{spec: spec, sessionID: id}
		states = append(states, st)
		bySession[id] = st
		fmt.Printf("  launched %s reviewer: %s\n", spec.name, id)
	}

	// 8. SSE state machine — two phases: reviewers -> coordinator.
	//    A single /event stream carries events for all sessions, routed by
	//    Properties.SessionID. When every reviewer is done (idle or errored)
	//    the coordinator session is launched and its deltas stream live.
	phase := "reviewers"
	var coordinatorID string
	for stream.Next() {
		ev := stream.Event()
		switch sse.EventType(ev) {

		case "permission.asked": // auto-approve so reviewers can use tools
			pa, err := sse.EventAs[opencode.EventPermissionAsked](ev)
			if err != nil {
				continue
			}
			resp, err := client.PermissionRespond(ctx, pa.Properties.SessionID,
				pa.Properties.Id, nil, opencode.PermissionRespondJSONRequestBody{
					Response: opencode.PermissionRespondJSONBodyResponseAlways,
				})
			if err != nil {
				log.Printf("permission respond: %v", err)
				continue
			}
			resp.Body.Close()

		case "session.next.tool.called": // log which reviewer is calling which tool
			tc, err := sse.EventAs[opencode.EventSessionNextToolCalled](ev)
			if err != nil {
				continue
			}
			if st, ok := bySession[tc.Properties.SessionID]; ok {
				fmt.Printf("  [%s] calling %s\n", st.spec.name, tc.Properties.Tool)
			}

		case "session.next.text.ended": // collect each reviewer's findings
			te, err := sse.EventAs[opencode.EventSessionNextTextEnded](ev)
			if err != nil {
				continue
			}
			if st, ok := bySession[te.Properties.SessionID]; ok {
				st.findings.WriteString(te.Properties.Text)
			}

		case "session.next.text.delta": // stream the coordinator's review live
			if phase == "coordinator" {
				d, err := sse.EventAs[opencode.EventSessionNextTextDelta](ev)
				if err != nil {
					continue
				}
				if d.Properties.SessionID == coordinatorID {
					fmt.Print(d.Properties.Delta)
				}
			}

		case "session.idle":
			si, err := sse.EventAs[opencode.EventSessionIdle](ev)
			if err != nil {
				continue
			}
			id := si.Properties.SessionID
			if st, ok := bySession[id]; ok {
				st.idle = true
				fmt.Printf("\n  [%s reviewer done]\n", st.spec.name)
				if phase == "reviewers" && allReviewersDone(states) {
					phase = "coordinator"
					coordinatorID = launchCoordinator(ctx, client, states, diff.String())
				}
			} else if id == coordinatorID {
				fmt.Println("\n\n— review complete —")
				return
			}

		case "session.error": // do NOT return — other sessions still run
			se, err := sse.EventAs[opencode.EventSessionError](ev)
			if err != nil {
				continue
			}
			// SessionID is *string here; error details live in an
			// unexported union and are inaccessible to custom code, so we
			// only route by SessionID.
			if se.Properties.SessionID == nil {
				continue
			}
			id := *se.Properties.SessionID
			if st, ok := bySession[id]; ok {
				st.errored = true
				fmt.Printf("\n  [%s reviewer errored — continuing with partial findings]\n",
					st.spec.name)
				// same transition as the idle path: a reviewer that errors
				// may never emit session.idle, so we must not wait for it.
				if phase == "reviewers" && allReviewersDone(states) {
					phase = "coordinator"
					coordinatorID = launchCoordinator(ctx, client, states, diff.String())
				}
			} else if phase == "coordinator" && id == coordinatorID {
				fmt.Println("\n\n[coordinator errored — partial review above]")
				return
			}
		}
	}
	if err := stream.Err(); err != nil {
		log.Printf("stream error: %v", err)
	}
}

// createSession creates an opencode session and returns its ID.
func createSession(ctx context.Context, client *opencode.Client, title string) (string, error) {
	resp, err := client.SessionCreate(ctx, nil, opencode.SessionCreateJSONRequestBody{
		Title: opencode.Ptr(title),
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var session opencode.Session
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return "", err
	}
	return session.Id, nil
}

// sendAsyncPrompt sends an asynchronous prompt to a session.
func sendAsyncPrompt(ctx context.Context, client *opencode.Client, sessionID, prompt string) error {
	resp, err := client.SessionPromptAsync(ctx, sessionID, nil,
		opencode.SessionPromptAsyncJSONRequestBody(
			opencode.TextPromptAsyncBody(prompt),
		),
	)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// launchCoordinator creates the coordinator session, sends it the
// consolidated findings from all reviewers, and returns its session ID.
// The caller streams the coordinator's deltas from the SSE loop.
func launchCoordinator(ctx context.Context, client *opencode.Client, states []*reviewerState, diff string) string {
	prompt := buildCoordinatorPrompt(states, diff)
	id, err := createSession(ctx, client, "Review coordinator")
	if err != nil {
		log.Fatalf("create coordinator session: %v", err)
	}
	if err := sendAsyncPrompt(ctx, client, id, prompt); err != nil {
		log.Fatalf("prompt async coordinator: %v", err)
	}
	fmt.Printf("\nCoordinator: %s — streaming final review...\n\n", id)
	return id
}

// buildCoordinatorPrompt assembles the coordinator's input: the rubric
// preamble, one <findings reviewer="key"> block per specialist (in launch
// order), and the diff for verification.
func buildCoordinatorPrompt(states []*reviewerState, diff string) string {
	var b strings.Builder
	b.WriteString(coordinatorPreamble)
	b.WriteString("\n")
	for _, st := range states {
		fmt.Fprintf(&b, "<findings reviewer=%q>\n", st.spec.key)
		if st.errored {
			b.WriteString("errored before producing findings\n")
		} else {
			b.WriteString(st.findings.String())
		}
		b.WriteString("</findings>\n\n")
	}
	b.WriteString("<diff>\n")
	b.WriteString(diff)
	b.WriteString("</diff>\n")
	return b.String()
}

// allReviewersDone reports whether every reviewer has finished. A reviewer
// that errors without ever going idle still counts as done (idle || errored)
// — otherwise the loop would deadlock on an errored reviewer whose
// session.idle never arrives.
func allReviewersDone(states []*reviewerState) bool {
	for _, s := range states {
		if !s.idle && !s.errored {
			return false
		}
	}
	return true
}

// assessRiskTier picks a tier based on diff size and security-sensitive paths.
// Security-sensitive files always force a full review.
func assessRiskTier(filtered []opencode.VcsFileDiff) string {
	totalLines := 0
	for _, d := range filtered {
		totalLines += int(d.Additions + d.Deletions)
	}
	for _, d := range filtered {
		if isSecuritySensitive(d.File) {
			return "full"
		}
	}
	if totalLines <= 10 && len(filtered) <= 20 {
		return "trivial"
	}
	if totalLines <= 100 && len(filtered) <= 20 {
		return "lite"
	}
	return "full"
}

// specsForTier returns the reviewer roster for a given risk tier.
func specsForTier(tier string) []reviewerSpec {
	switch tier {
	case "trivial":
		return []reviewerSpec{codeQualitySpec}
	case "lite":
		return []reviewerSpec{codeQualitySpec, securitySpec}
	default:
		return reviewerSpecs
	}
}

// specNames joins reviewer display names with commas.
func specNames(specs []reviewerSpec) string {
	parts := make([]string, len(specs))
	for i, s := range specs {
		parts[i] = s.name
	}
	return strings.Join(parts, ", ")
}

// filterDiff drops noise files (lock files, minified assets, source maps).
// Migrations are exempt: migration files often carry a generated marker but
// still need review.
func filterDiff(diffs []opencode.VcsFileDiff) (filtered []opencode.VcsFileDiff, skipped int) {
	for _, d := range diffs {
		if isNoiseFile(d.File) {
			skipped++
			continue
		}
		filtered = append(filtered, d)
	}
	return filtered, skipped
}

// isNoiseFile reports whether a path is noise that should be stripped
// before review.
func isNoiseFile(path string) bool {
	// Migrations are exempt even if they look generated.
	if strings.Contains(strings.ToLower(path), "migration") {
		return false
	}
	switch filepath.Base(path) {
	case "go.sum", "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "Cargo.lock":
		return true
	}
	for _, suf := range []string{".min.js", ".min.css", ".map"} {
		if strings.HasSuffix(path, suf) {
			return true
		}
	}
	return false
}

// isSecuritySensitive reports whether a path touches security-relevant code.
func isSecuritySensitive(path string) bool {
	p := strings.ToLower(path)
	for _, s := range []string{"auth", "crypto", "secret", "token", "permission", "password", "cred"} {
		if strings.Contains(p, s) {
			return true
		}
	}
	return false
}
