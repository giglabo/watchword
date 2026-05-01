package auth

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"

	"github.com/watchword/watchword/internal/config"
	"github.com/watchword/watchword/internal/domain"
)

type Authenticator struct {
	enabled             bool
	tokens              []string
	namedTokens         []config.NamedToken
	jwt                 *JWTValidator
	resourceMetadataURL string
}

func NewAuthenticator(enabled bool, tokens []string, named []config.NamedToken) *Authenticator {
	return &Authenticator{
		enabled:     enabled,
		tokens:      tokens,
		namedTokens: named,
	}
}

func (a *Authenticator) SetJWTValidator(v *JWTValidator) {
	a.jwt = v
}

// SetResourceMetadataURL sets the URL returned in WWW-Authenticate headers
// (e.g. "https://watchword.example.com/.well-known/oauth-protected-resource").
func (a *Authenticator) SetResourceMetadataURL(url string) {
	a.resourceMetadataURL = url
}

// Validate verifies the bearer token and returns the caller's identity.
// Identity is the JWT identity claim (e.g. `sub`) for OAuth tokens, the
// configured name for a `named_tokens` entry, or "" for an unnamed static
// token / when auth is disabled. An empty identity means anonymous, not
// failure — failures return ErrUnauthorized.
func (a *Authenticator) Validate(token string) (string, error) {
	if !a.enabled {
		return "", nil
	}
	if token == "" {
		return "", domain.ErrUnauthorized
	}

	if a.jwt != nil && strings.Count(token, ".") == 2 {
		if id, err := a.jwt.Validate(token); err == nil {
			return id, nil
		}
	}

	for _, nt := range a.namedTokens {
		if subtle.ConstantTimeCompare([]byte(token), []byte(nt.Token)) == 1 {
			return nt.Name, nil
		}
	}
	for _, valid := range a.tokens {
		if subtle.ConstantTimeCompare([]byte(token), []byte(valid)) == 1 {
			return "", nil
		}
	}
	return "", domain.ErrUnauthorized
}

func (a *Authenticator) HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.enabled {
			next.ServeHTTP(w, r)
			return
		}

		// Skip auth for well-known discovery endpoints
		if strings.HasPrefix(r.URL.Path, "/.well-known/") {
			next.ServeHTTP(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			a.writeUnauthorized(w, "missing auth token")
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == authHeader {
			a.writeUnauthorized(w, "invalid authorization format")
			return
		}

		identity, err := a.Validate(token)
		if err != nil {
			a.writeUnauthorized(w, "invalid auth token")
			return
		}

		if identity != "" {
			r = r.WithContext(WithIdentity(r.Context(), identity))
		}
		next.ServeHTTP(w, r)
	})
}

func (a *Authenticator) writeUnauthorized(w http.ResponseWriter, msg string) {
	wwwAuth := "Bearer"
	if a.resourceMetadataURL != "" {
		wwwAuth = fmt.Sprintf(`Bearer resource_metadata=%q`, a.resourceMetadataURL)
	}
	w.Header().Set("WWW-Authenticate", wwwAuth)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	fmt.Fprintf(w, `{"error":"unauthorized: %s"}`, msg)
}
