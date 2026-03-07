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

	if err := v.Validate(token); err != nil {
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

	if err := v.Validate(token); err == nil {
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

	if err := v.Validate(token); err == nil {
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

	if err := v.Validate(token); err == nil {
		t.Error("token with wrong audience should fail")
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

	if err := v.Validate(token); err == nil {
		t.Error("token with wrong signature should fail")
	}
}
