package opencode

import (
	"context"
	"net/http"
)

// WithBasicAuth adds HTTP Basic authentication to every request made by the
// client. The opencode server supports password-based authentication when
// configured with a password.
//
//	client, _ := opencode.NewClient("http://localhost:4096", opencode.WithBasicAuth("s3cret"))
func WithBasicAuth(password string) ClientOption {
	return func(c *Client) error {
		c.RequestEditors = append(c.RequestEditors, func(ctx context.Context, req *http.Request) error {
			req.SetBasicAuth("", password)
			return nil
		})
		return nil
	}
}

// WithRequestEditor adds a custom request editor to the client. This is a
// convenience wrapper around the generated WithRequestEditorFn for users
// who want to add headers or modify requests globally.
func WithRequestEditor(fn RequestEditorFn) ClientOption {
	return WithRequestEditorFn(fn)
}
