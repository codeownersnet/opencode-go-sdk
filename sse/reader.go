// Package sse provides a first-class SSE (Server-Sent Events) reader for the
// OpenCode server's /event and /global/event endpoints.
//
// The generated client returns raw *http.Response for these endpoints; this
// package wraps the response body in a typed stream that decodes each SSE
// frame into an opencode.Event or opencode.GlobalEvent, with helpers for
// discriminating the event variant via the "type" field.
package sse

import (
	"bufio"
	"io"
	"strings"
)

// frame represents a single Server-Sent Event frame.
type frame struct {
	event string // the "event:" field (rarely used by opencode)
	data  strings.Builder
	id    string
	retry int
}

// reader reads SSE frames from an io.Reader.
type reader struct {
	scanner *bufio.Scanner
	frame   frame
	err     error
}

func newReader(r io.Reader) *reader {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return &reader{scanner: s}
}

// next reads the next complete SSE frame. Returns false on EOF or error.
func (r *reader) next() bool {
	r.frame = frame{}
	var dataLines []string

	for r.scanner.Scan() {
		line := r.scanner.Text()

		// Empty line dispatches the event.
		if line == "" {
			if len(dataLines) == 0 {
				continue // ignore keep-alive comments
			}
			r.frame.data.WriteString(strings.Join(dataLines, "\n"))
			return true
		}

		switch {
		case strings.HasPrefix(line, ":"):
			// Comment line (keep-alive); ignore.
			continue
		case strings.HasPrefix(line, "data:"):
			dataLines = append(dataLines, strings.TrimPrefix(line, "data:"))
		case strings.HasPrefix(line, "event:"):
			r.frame.event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "id:"):
			r.frame.id = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
		case strings.HasPrefix(line, "retry:"):
			// Retry field is informational; we don't auto-reconnect here.
		default:
			// Unknown field; ignore per SSE spec.
		}
	}

	if err := r.scanner.Err(); err != nil {
		r.err = err
	} else if len(dataLines) > 0 {
		// EOF with a pending event (no trailing blank line).
		r.frame.data.WriteString(strings.Join(dataLines, "\n"))
		return true
	}
	return false
}

// data returns the current frame's data payload.
func (r *reader) data() []byte {
	return []byte(r.frame.data.String())
}
