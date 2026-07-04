package opencode_test

import (
	"encoding/json"
	"os"
	"testing"
)

// TestEndpointCount ensures the committed spec has a minimum number of paths
// and operations. This catches accidental trimming or a misconfigured pull.
// The upstream spec currently exposes 162 paths and 188 operations; the floor
// gives a small margin for temporary upstream churn.
func TestEndpointCount(t *testing.T) {
	data, err := os.ReadFile("opencode-spec.json")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	var spec struct {
		Paths map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}
	const minPaths = 160
	if got := len(spec.Paths); got < minPaths {
		t.Errorf("spec has %d paths, want at least %d — the upstream spec may have been trimmed or the pull failed", got, minPaths)
	}
	ops := 0
	methods := map[string]bool{"get": true, "post": true, "put": true, "delete": true, "patch": true}
	for _, p := range spec.Paths {
		for m := range p {
			if methods[m] {
				ops++
			}
		}
	}
	const minOps = 185
	if ops < minOps {
		t.Errorf("spec has %d operations, want at least %d", ops, minOps)
	}
}
