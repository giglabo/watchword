package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/watchword/watchword/internal/config"
)

func TestAuthenticator_Validate(t *testing.T) {
	a := NewAuthenticator(true, []string{"token1", "token2"})

	if err := a.Validate("token1"); err != nil {
		t.Errorf("valid token1 should pass: %v", err)
	}
	if err := a.Validate("token2"); err != nil {
		t.Errorf("valid token2 should pass: %v", err)
	}
	if err := a.Validate("invalid"); err == nil {
		t.Error("invalid token should fail")
	}
	if err := a.Validate(""); err == nil {
		t.Error("empty token should fail")
	}
}

func TestAuthenticator_Disabled(t *testing.T) {
	a := NewAuthenticator(false, nil)
	if err := a.Validate(""); err != nil {
		t.Errorf("disabled auth should pass: %v", err)
	}
	if err := a.Validate("anything"); err != nil {
		t.Errorf("disabled auth should pass: %v", err)
	}
}

func TestAuthenticator_JWTDispatch(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	kid := "test-key-1"
	srv := setupJWKSServer(t, &key.PublicKey, kid)

	jwtVal, err := NewJWTValidator(context.Background(), &config.JWTConfig{
		JWKSURL: srv.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer jwtVal.Close()

	a := NewAuthenticator(true, []string{"plain-token"})
	a.SetJWTValidator(jwtVal)

	// Valid JWT should pass
	jwtToken := signToken(t, key, kid, jwt.MapClaims{
		"sub": "user1",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	if err := a.Validate(jwtToken); err != nil {
		t.Errorf("valid JWT should pass: %v", err)
	}

	// Plain token should still work
	if err := a.Validate("plain-token"); err != nil {
		t.Errorf("plain token should pass: %v", err)
	}

	// Invalid token should fail
	if err := a.Validate("bad-token"); err == nil {
		t.Error("invalid token should fail")
	}
}

func TestAuthenticator_JWTFallbackToPlain(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	kid := "test-key-1"
	srv := setupJWKSServer(t, &key.PublicKey, kid)

	jwtVal, err := NewJWTValidator(context.Background(), &config.JWTConfig{
		JWKSURL: srv.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer jwtVal.Close()

	// Use a plain token that looks like a JWT (has 2 dots)
	a := NewAuthenticator(true, []string{"a.b.c"})
	a.SetJWTValidator(jwtVal)

	// "a.b.c" has 2 dots, so JWT is tried first but fails, then falls back to plain
	if err := a.Validate("a.b.c"); err != nil {
		t.Errorf("plain token with dots should fall back to plain validation: %v", err)
	}
}

func TestHTTPMiddleware_SkipsWellKnown(t *testing.T) {
	a := NewAuthenticator(true, []string{"secret"})

	handler := a.HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Request to .well-known should pass without auth
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("well-known path should skip auth, got %d", w.Code)
	}

	// Request to .well-known/oauth-protected-resource should also pass
	req = httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("well-known protected-resource path should skip auth, got %d", w.Code)
	}

	// Request to /mcp without auth should fail
	req = httptest.NewRequest(http.MethodGet, "/mcp", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("protected path without auth should return 401, got %d", w.Code)
	}

	// Request to /mcp with valid auth should pass
	req = httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer secret")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("protected path with valid auth should return 200, got %d", w.Code)
	}
}

func TestHTTPMiddleware_WWWAuthenticate_Plain(t *testing.T) {
	a := NewAuthenticator(true, []string{"secret"})

	handler := a.HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	wwwAuth := w.Header().Get("WWW-Authenticate")
	if wwwAuth != "Bearer" {
		t.Errorf("expected WWW-Authenticate: Bearer, got %q", wwwAuth)
	}
}

func TestHTTPMiddleware_WWWAuthenticate_WithResourceMetadata(t *testing.T) {
	a := NewAuthenticator(true, []string{"secret"})
	a.SetResourceMetadataURL("https://watchword.example.com/.well-known/oauth-protected-resource")

	handler := a.HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	wwwAuth := w.Header().Get("WWW-Authenticate")
	expected := `Bearer resource_metadata="https://watchword.example.com/.well-known/oauth-protected-resource"`
	if wwwAuth != expected {
		t.Errorf("WWW-Authenticate:\n  got:  %q\n  want: %q", wwwAuth, expected)
	}
}
