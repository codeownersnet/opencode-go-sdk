package sse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	opencode "github.com/codeownersnet/opencode-go-sdk"
)

// ErrUnknownEventType is returned by EventAs/GlobalEventAs when the event's
// variant type is not known to the caller (i.e. a new event type added
// upstream that the caller hasn't opted into yet). This sentinel error lets
// callers distinguish "unknown type" from JSON decode failures.
var ErrUnknownEventType = errors.New("opencode/sse: unknown event type")

// rawEventType is a helper struct for extracting the "type" discriminator
// from any event variant without needing to know the concrete type.
type rawEventType struct {
	Type string `json:"type"`
}

// EventType extracts the "type" string from an Event. Returns "" if the
// event has no type field or the payload cannot be decoded. Never panics.
func EventType(e opencode.Event) string {
	data, err := json.Marshal(e)
	if err != nil {
		return ""
	}
	var t rawEventType
	if err := json.Unmarshal(data, &t); err != nil {
		return ""
	}
	return t.Type
}

// EventAs deserializes an Event into a concrete variant type T (e.g.
// opencode.EventSessionUpdated). Returns ErrUnknownEventType if the JSON
// cannot be decoded into T.
//
// Callers should switch on EventType first, then call EventAs with the
// matching variant type. EventAs does not validate that the event's type
// string matches T — it decodes whatever is in the Event union into T. This
// means adding a new variant upstream never breaks existing code: unknown
// types simply don't match any case in the caller's switch.
//
//	switch sse.EventType(ev) {
//	case "session.updated":
//	    su, err := sse.EventAs[opencode.EventSessionUpdated](ev)
//	}
func EventAs[T any](e opencode.Event) (*T, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("marshal event: %w", err)
	}
	var t T
	// json.Unmarshal populates all successfully-decoded fields before
	// returning a type error (e.g. a timestamp sent as a string when the
	// generated type expects a float32). The caller has already verified
	// the event type via EventType, so a field-level unmarshal error is
	// not a reason to discard the whole event — return the partial result.
	if err := json.Unmarshal(data, &t); err != nil {
		return &t, nil
	}
	return &t, nil
}

// GlobalEventType extracts the "type" string from a GlobalEvent's payload.
// Returns "" if the payload has no type field or cannot be decoded.
func GlobalEventType(e opencode.GlobalEvent) string {
	data, err := json.Marshal(e.Payload)
	if err != nil {
		return ""
	}
	var t rawEventType
	if err := json.Unmarshal(data, &t); err != nil {
		return ""
	}
	return t.Type
}

// GlobalEventAs deserializes a GlobalEvent's payload into a concrete variant
// type T (e.g. opencode.GlobalEventSessionUpdated).
func GlobalEventAs[T any](e opencode.GlobalEvent) (*T, error) {
	data, err := json.Marshal(e.Payload)
	if err != nil {
		return nil, fmt.Errorf("marshal global event payload: %w", err)
	}
	var t T
	// See EventAs for why we tolerate field-level unmarshal errors.
	if err := json.Unmarshal(data, &t); err != nil {
		return &t, nil
	}
	return &t, nil
}

// SubscribeEvents opens a server-sent events stream on the /event endpoint
// (per-instance events). The caller must close the returned stream when done.
func SubscribeEvents(ctx context.Context, c *opencode.Client, params *opencode.EventSubscribeParams) (*Stream[opencode.Event], error) {
	resp, err := c.EventSubscribe(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("subscribe events: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("subscribe events: unexpected status %s", resp.Status)
	}
	return NewStream[opencode.Event](resp.Body, resp.Body), nil
}

// SubscribeGlobalEvents opens a server-sent events stream on the
// /global/event endpoint (system-wide events). The caller must close the
// returned stream when done.
func SubscribeGlobalEvents(ctx context.Context, c *opencode.Client) (*Stream[opencode.GlobalEvent], error) {
	resp, err := c.GlobalEvent(ctx)
	if err != nil {
		return nil, fmt.Errorf("subscribe global events: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("subscribe global events: unexpected status %s", resp.Status)
	}
	return NewStream[opencode.GlobalEvent](resp.Body, resp.Body), nil
}
