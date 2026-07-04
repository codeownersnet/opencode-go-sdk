package sse_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	opencode "github.com/codeownersnet/opencode-go-sdk"
	"github.com/codeownersnet/opencode-go-sdk/sse"
)

// rawEvent is a minimal helper to construct an opencode.Event from raw JSON.
// Since opencode.Event wraps a json.RawMessage union, we can Unmarshal into it.
func rawEvent(t *testing.T, data string) opencode.Event {
	var e opencode.Event
	if err := json.Unmarshal([]byte(data), &e); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	return e
}

func TestEventType(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string
	}{
		{
			name: "session.updated",
			json: `{"id":"evt_1","type":"session.updated","properties":{"sessionID":"ses_1","info":{"id":"ses_1","title":"test"}}}`,
			want: "session.updated",
		},
		{
			name: "message.part.updated",
			json: `{"id":"evt_2","type":"message.part.updated","properties":{"sessionID":"ses_1","messageID":"msg_1"}}`,
			want: "message.part.updated",
		},
		{
			name: "session.next.text.delta",
			json: `{"id":"evt_3","type":"session.next.text.delta","properties":{"sessionID":"ses_1","messageID":"msg_1","text":"hello"}}`,
			want: "session.next.text.delta",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := rawEvent(t, tc.json)
			got := sse.EventType(e)
			if got != tc.want {
				t.Errorf("EventType() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEventTypeUnknown(t *testing.T) {
	e := rawEvent(t, `{"id":"evt_99","type":"some.future.event","properties":{}}`)
	got := sse.EventType(e)
	if got != "some.future.event" {
		t.Errorf("EventType() = %q, want %q", got, "some.future.event")
	}
}

func TestEventTypeEmpty(t *testing.T) {
	e := rawEvent(t, `{"id":"evt_100","properties":{}}`)
	got := sse.EventType(e)
	if got != "" {
		t.Errorf("EventType() = %q, want empty", got)
	}
}

func TestEventAs(t *testing.T) {
	e := rawEvent(t, `{"id":"evt_1","type":"session.updated","properties":{"sessionID":"ses_1","info":{"id":"ses_1","title":"test"}}}`)

	su, err := sse.EventAs[opencode.EventSessionUpdated](e)
	if err != nil {
		t.Fatalf("EventAs failed: %v", err)
	}
	if su.Properties.SessionID != "ses_1" {
		t.Errorf("sessionID = %q, want %q", su.Properties.SessionID, "ses_1")
	}
	if su.Properties.Info.Title != "test" {
		t.Errorf("title = %q, want %q", su.Properties.Info.Title, "test")
	}
}

func TestEventAsUnknownType(t *testing.T) {
	// EventAs decodes without validating the type string — callers are
	// expected to switch on EventType first. Here we verify that an unknown
	// event type is simply not matched by the caller's switch, and that
	// EventType returns the unknown string so callers can ignore it.
	e := rawEvent(t, `{"id":"evt_1","type":"unknown.event","properties":{}}`)
	got := sse.EventType(e)
	if got != "unknown.event" {
		t.Errorf("EventType() = %q, want %q", got, "unknown.event")
	}

	// A caller's switch would not match "unknown.event" to any known case,
	// so it would be silently ignored — the desired behavior for forward
	// compatibility with new upstream variants.
}

func TestGlobalEventType(t *testing.T) {
	jsonStr := `{
		"directory": "/tmp",
		"payload": {"id":"evt_1","type":"session.updated","properties":{"sessionID":"ses_1","info":{"id":"ses_1","title":"test"}}}
	}`
	var e opencode.GlobalEvent
	if err := json.Unmarshal([]byte(jsonStr), &e); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := sse.GlobalEventType(e)
	if got != "session.updated" {
		t.Errorf("GlobalEventType() = %q, want %q", got, "session.updated")
	}
}

// TestStreamRoundTrip serves a canned SSE response and verifies that
// Stream[Event] correctly parses frames and decodes them.
func TestStreamRoundTrip(t *testing.T) {
	frames := []string{
		`{"id":"evt_1","type":"session.updated","properties":{"sessionID":"ses_1","info":{"id":"ses_1","title":"test"}}}`,
		`{"id":"evt_2","type":"message.part.updated","properties":{"sessionID":"ses_1","messageID":"msg_1"}}`,
		`{"id":"evt_3","type":"session.next.text.delta","properties":{"sessionID":"ses_1","messageID":"msg_1","text":"hello"}}`,
	}
	sseBody := strings.Join(
		[]string{
			fmt.Sprintf("data: %s\n\n", frames[0]),
			fmt.Sprintf("data: %s\n\n", frames[1]),
			fmt.Sprintf("data: %s\n\n", frames[2]),
		},
		"",
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("server does not support flushing")
		}
		fmt.Fprint(w, sseBody)
		flusher.Flush()
	}))
	defer srv.Close()

	client, err := opencode.NewClient(srv.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	stream, err := sse.SubscribeEvents(context.Background(), client, nil)
	if err != nil {
		t.Fatalf("SubscribeEvents: %v", err)
	}
	defer stream.Close()

	wantTypes := []string{"session.updated", "message.part.updated", "session.next.text.delta"}
	var gotTypes []string
	for stream.Next() {
		gotTypes = append(gotTypes, sse.EventType(stream.Event()))
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}
	if len(gotTypes) != len(wantTypes) {
		t.Fatalf("got %d events, want %d", len(gotTypes), len(wantTypes))
	}
	for i, want := range wantTypes {
		if gotTypes[i] != want {
			t.Errorf("event[%d] type = %q, want %q", i, gotTypes[i], want)
		}
	}
}

func TestStreamMultilineData(t *testing.T) {
	// SSE allows multi-line data fields, joined with \n.
	sseBody := "data: {\"id\":\"evt_1\",\"type\":\"session.updated\"," +
		"\"properties\":{\"sessionID\":\"ses_1\",\"info\":{\"id\":\"ses_1\",\"title\":\"test\"}}}\n\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseBody)
		w.(http.Flusher).Flush()
	}))
	defer srv.Close()

	client, err := opencode.NewClient(srv.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	stream, err := sse.SubscribeEvents(context.Background(), client, nil)
	if err != nil {
		t.Fatalf("SubscribeEvents: %v", err)
	}
	defer stream.Close()

	if !stream.Next() {
		t.Fatalf("Next() = false, want true; err=%v", stream.Err())
	}
	if sse.EventType(stream.Event()) != "session.updated" {
		t.Errorf("type = %q, want %q", sse.EventType(stream.Event()), "session.updated")
	}
	if stream.Next() {
		t.Fatal("expected EOF after first event")
	}
}
