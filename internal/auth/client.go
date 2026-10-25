package auth

import "context"

// Client describes where a request came from. It is recorded with sessions
// and in the audit log.
type Client struct {
	Remote    string
	UserAgent string
}

type clientKey struct{}

// WithClient returns a context that carries the client of a request, for the
// service methods that record it. The HTTP handlers do this themselves.
func WithClient(ctx context.Context, c Client) context.Context {
	return context.WithValue(ctx, clientKey{}, c)
}

func clientFrom(ctx context.Context) Client {
	c, _ := ctx.Value(clientKey{}).(Client)
	return c
}

// clip shortens a text that came from a client, so it cannot bloat the
// database.
func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	r := []rune(s)
	if len(r) > max {
		r = r[:max]
	}
	return string(r)
}
