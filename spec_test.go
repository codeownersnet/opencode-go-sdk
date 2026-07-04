package opencode_test

import (
	"encoding/json"
	"os"
	"testing"
)

// TestEndpointCount ensures the committed spec has a minimum number of paths.
// This catches accidental trimming or a misconfigured pull. The upstream spec
// currently exposes ~131 paths; we require at least 125 to allow for minor
// upstream changes without breaking this test.
func TestEndpointCount(t *testing.T) {
	data, err := os.ReadFile("opencode-spec.json")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	var spec struct {
		Paths map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}
	const minPaths = 125
	if got := len(spec.Paths); got < minPaths {
		t.Errorf("spec has %d paths, want at least %d — the upstream spec may have been trimmed or the pull failed", got, minPaths)
	}
}
