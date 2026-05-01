package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/watchword/watchword/internal/config"
)

// rsaPublicJWKS returns a JWKS JSON with the given RSA public key.
func rsaPublicJWKS(t *testing.T, key *rsa.PublicKey, kid string) []byte {
	t.Helper()
	jwks := map[string]any{
		"keys": []map[string]any{
			{
				"kty": "RSA",
				"use": "sig",
				"alg": "RS256",
				"kid": kid,
				"n":   base64URLEncode(key.N.Bytes()),
				"e":   base64URLEncode(big.NewInt(int64(key.E)).Bytes()),
			},
		},
	}
	data, err := json.Marshal(jwks)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func base64URLEncode(data []byte) string {
	const enc = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	result := make([]byte, 0, (len(data)*8+5)/6)
	var bits uint64
	var nBits int
	for _, b := range data {
		bits = (bits << 8) | uint64(b)
		nBits += 8
		for nBits >= 6 {
			nBits -= 6
			result = append(result, enc[(bits>>uint(nBits))&0x3f])
		}
	}
	if nBits > 0 {
		result = append(result, enc[(bits<<uint(6-nBits))&0x3f])
	}
	return string(result)
}

func setupJWKSServer(t *testing.T, key *rsa.PublicKey, kid string) *httptest.Server {
	t.Helper()
	jwksData := rsaPublicJWKS(t, key, kid)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(jwksData)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func signToken(t *testing.T, key *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func TestJWTValidator_ValidToken(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	kid := "test-key-1"
	srv := setupJWKSServer(t, &key.PublicKey, kid)

	v, err := NewJWTValidator(context.Background(), &config.JWTConfig{
		JWKSURL: srv.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()

	token := signToken(t, key, kid, jwt.MapClaims{
		"sub": "user1",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	if _, err := v.Validate(token); err != nil {
		t.Errorf("valid token should pass: %v", err)
	}
}

func TestJWTValidator_ExpiredToken(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	kid := "test-key-1"
	srv := setupJWKSServer(t, &key.PublicKey, kid)

	v, err := NewJWTValidator(context.Background(), &config.JWTConfig{
		JWKSURL: srv.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()

	token := signToken(t, key, kid, jwt.MapClaims{
		"sub": "user1",
		"exp": time.Now().Add(-time.Hour).Unix(),
	})

	if _, err := v.Validate(token); err == nil {
		t.Error("expired token should fail")
	}
}

func TestJWTValidator_WrongIssuer(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	kid := "test-key-1"
	srv := setupJWKSServer(t, &key.PublicKey, kid)

	v, err := NewJWTValidator(context.Background(), &config.JWTConfig{
		JWKSURL: srv.URL,
		Issuer:  "https://expected-issuer.com/",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()

	token := signToken(t, key, kid, jwt.MapClaims{
		"sub": "user1",
		"iss": "https://wrong-issuer.com/",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	if _, err := v.Validate(token); err == nil {
		t.Error("token with wrong issuer should fail")
	}
}

func TestJWTValidator_WrongAudience(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	kid := "test-key-1"
	srv := setupJWKSServer(t, &key.PublicKey, kid)

	v, err := NewJWTValidator(context.Background(), &config.JWTConfig{
		JWKSURL:  srv.URL,
		Audience: "expected-audience",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()

	token := signToken(t, key, kid, jwt.MapClaims{
		"sub": "user1",
		"aud": "wrong-audience",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	if _, err := v.Validate(token); err == nil {
		t.Error("token with wrong audience should fail")
	}
}

func TestJWTValidator_RequiredScopes_ScopeString(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	kid := "test-key-1"
	srv := setupJWKSServer(t, &key.PublicKey, kid)

	v, err := NewJWTValidator(context.Background(), &config.JWTConfig{
		JWKSURL:        srv.URL,
		RequiredScopes: []string{"watchword:read", "watchword:write"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()

	good := signToken(t, key, kid, jwt.MapClaims{
		"sub":   "user1",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"scope": "watchword:read watchword:write extra",
	})
	if _, err := v.Validate(good); err != nil {
		t.Errorf("token with required scopes should pass: %v", err)
	}

	bad := signToken(t, key, kid, jwt.MapClaims{
		"sub":   "user1",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"scope": "watchword:read",
	})
	if _, err := v.Validate(bad); err == nil {
		t.Error("token missing required scope should fail")
	}

	none := signToken(t, key, kid, jwt.MapClaims{
		"sub": "user1",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	if _, err := v.Validate(none); err == nil {
		t.Error("token with no scope claim should fail when scopes required")
	}
}

func TestJWTValidator_RequiredScopes_ScpArray(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	kid := "test-key-1"
	srv := setupJWKSServer(t, &key.PublicKey, kid)

	v, err := NewJWTValidator(context.Background(), &config.JWTConfig{
		JWKSURL:        srv.URL,
		RequiredScopes: []string{"watchword:read"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()

	good := signToken(t, key, kid, jwt.MapClaims{
		"sub": "user1",
		"exp": time.Now().Add(time.Hour).Unix(),
		"scp": []any{"watchword:read", "other"},
	})
	if _, err := v.Validate(good); err != nil {
		t.Errorf("token with required scope in scp should pass: %v", err)
	}
}

func TestJWTValidator_IdentityClaim(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	kid := "test-key-1"
	srv := setupJWKSServer(t, &key.PublicKey, kid)

	// Default: sub
	defaultV, err := NewJWTValidator(context.Background(), &config.JWTConfig{JWKSURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer defaultV.Close()

	tok := signToken(t, key, kid, jwt.MapClaims{
		"sub":   "auth0|abc",
		"email": "alice@example.com",
		"exp":   time.Now().Add(time.Hour).Unix(),
	})
	id, err := defaultV.Validate(tok)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if id != "auth0|abc" {
		t.Errorf("default identity should be sub claim, got %q", id)
	}

	// Custom: email
	emailV, err := NewJWTValidator(context.Background(), &config.JWTConfig{
		JWKSURL:       srv.URL,
		IdentityClaim: "email",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer emailV.Close()
	id, err = emailV.Validate(tok)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if id != "alice@example.com" {
		t.Errorf("identity_claim=email should yield email, got %q", id)
	}
}

func TestJWTValidator_InvalidSignature(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	kid := "test-key-1"
	srv := setupJWKSServer(t, &key.PublicKey, kid)

	v, err := NewJWTValidator(context.Background(), &config.JWTConfig{
		JWKSURL: srv.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()

	// Sign with a different key
	token := signToken(t, otherKey, kid, jwt.MapClaims{
		"sub": "user1",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	if _, err := v.Validate(token); err == nil {
		t.Error("token with wrong signature should fail")
	}
}
