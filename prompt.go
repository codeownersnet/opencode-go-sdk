package opencode

import "encoding/json"

// Ptr returns a pointer to v. Useful for optional fields in the generated
// request structs:
//
//	params := opencode.SessionListParams{Limit: opencode.Ptr(50)}
func Ptr[T any](v T) *T {
	return &v
}

// TextPromptBody builds a SessionPromptJSONBody with a single text part —
// the most common usage. The prompt is synchronous (the server returns an
// AssistantMessage).
//
//	body := opencode.TextPromptBody("Explain this function")
//	resp, _ := client.SessionPrompt(ctx, sessionID, body, nil)
func TextPromptBody(text string) SessionPromptJSONBody {
	return SessionPromptJSONBody{
		Parts: []SessionPromptJSONBody_Parts_Item{
			{union: mustMarshalTextPart(text)},
		},
	}
}

// TextPromptAsyncBody builds a SessionPromptAsyncJSONBody with a single text
// part for asynchronous prompts. Use this when you don't want to block on the
// AI response and instead subscribe to events via SSE.
func TextPromptAsyncBody(text string) SessionPromptAsyncJSONBody {
	return SessionPromptAsyncJSONBody{
		Parts: []SessionPromptAsyncJSONBody_Parts_Item{
			{union: mustMarshalTextPart(text)},
		},
	}
}

// mustMarshalTextPart marshals a TextPartInput and panics on failure (which
// can only happen if the TextPartInput struct is malformed, which is a
// programming error, not a runtime error).
func mustMarshalTextPart(text string) json.RawMessage {
	part := TextPartInput{
		Type: TextPartInputTypeText,
		Text: text,
	}
	b, err := json.Marshal(part)
	if err != nil {
		panic("opencode: failed to marshal TextPartInput: " + err.Error())
	}
	return b
}

// MarshalJSON returns the underlying union value. Without this, json.Marshal
// ignores the unexported union field and produces {}.
func (i SessionPromptJSONBody_Parts_Item) MarshalJSON() ([]byte, error) {
	if len(i.union) == 0 {
		return []byte("null"), nil
	}
	return i.union, nil
}

// UnmarshalJSON stores a copy of the raw JSON for later round-tripping.
func (i *SessionPromptJSONBody_Parts_Item) UnmarshalJSON(data []byte) error {
	i.union = append(i.union[:0], data...)
	return nil
}

// MarshalJSON returns the underlying union value. Without this, json.Marshal
// ignores the unexported union field and produces {}.
func (i SessionPromptAsyncJSONBody_Parts_Item) MarshalJSON() ([]byte, error) {
	if len(i.union) == 0 {
		return []byte("null"), nil
	}
	return i.union, nil
}

// UnmarshalJSON stores a copy of the raw JSON for later round-tripping.
func (i *SessionPromptAsyncJSONBody_Parts_Item) UnmarshalJSON(data []byte) error {
	i.union = append(i.union[:0], data...)
	return nil
}
