package sse

import (
	"encoding/json"
	"io"
)

// Stream is a generic iterator over SSE events. Each call to Next reads one
// SSE frame and decodes its data payload as JSON into type T.
//
// Typical usage:
//
//	stream, _ := sse.SubscribeGlobalEvents(ctx, client)
//	defer stream.Close()
//	for stream.Next() {
//	    ev := stream.Event()
//	    switch sse.GlobalEventType(ev) {
//	    case "session.updated":
//	        su, _ := sse.GlobalEventAs[GlobalEventSessionUpdated](ev)
//	        // ...
//	    }
//	}
//	if err := stream.Err(); err != nil {
//	    // handle error
//	}
type Stream[T any] struct {
	r       *reader
	closer  io.Closer
	current T
	err     error
	done    bool
}

// NewStream wraps an io.Reader (typically an *http.Response.Body) into a
// typed SSE stream. The closer is closed when Close is called; pass the
// *http.Response.Body here so it gets cleaned up.
func NewStream[T any](r io.Reader, closer io.Closer) *Stream[T] {
	return &Stream[T]{
		r:      newReader(r),
		closer: closer,
	}
}

// Next reads the next SSE frame and decodes it into T. Returns false at EOF
// or on error (check Err).
func (s *Stream[T]) Next() bool {
	if s.done {
		return false
	}
	if !s.r.next() {
		s.done = true
		if s.err == nil {
			s.err = s.r.err
		}
		return false
	}
	data := s.r.data()
	if len(data) == 0 {
		// Empty data frame; skip.
		return s.Next()
	}
	if err := json.Unmarshal(data, &s.current); err != nil {
		s.err = err
		s.done = true
		return false
	}
	return true
}

// Event returns the most recent event decoded by Next.
func (s *Stream[T]) Event() T {
	return s.current
}

// Err returns any error encountered during iteration. Returns nil if the
// stream ended cleanly at EOF.
func (s *Stream[T]) Err() error {
	return s.err
}

// Close closes the underlying response body.
func (s *Stream[T]) Close() error {
	if s.closer != nil {
		return s.closer.Close()
	}
	return nil
}
