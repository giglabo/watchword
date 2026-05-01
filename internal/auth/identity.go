package auth

import "context"

type identityKey struct{}

// WithIdentity returns a context carrying the caller's identity (typically a
// JWT claim like `sub`/`email`, or the name of a named static token).
// An empty id is treated as anonymous and not stored.
func WithIdentity(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, identityKey{}, id)
}

// IdentityFrom extracts the caller's identity from ctx. Returns ("", false)
// when the request is anonymous (e.g. unnamed static token, or auth disabled).
func IdentityFrom(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(identityKey{}).(string)
	if !ok || id == "" {
		return "", false
	}
	return id, true
}
