package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/watchword/watchword/internal/config"
)

func TestOAuthMetadataHandler(t *testing.T) {
	handler := OAuthMetadataHandler(
		"https://example.com/.well-known/jwks.json",
		"https://example.com/",
		&config.OAuthMetadataConfig{
			AuthorizationEndpoint: "https://example.com/authorize",
			TokenEndpoint:         "https://example.com/oauth/token",
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %s", ct)
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	expected := map[string]string{
		"issuer":                 "https://example.com/",
		"authorization_endpoint": "https://example.com/authorize",
		"token_endpoint":         "https://example.com/oauth/token",
		"jwks_uri":               "https://example.com/.well-known/jwks.json",
	}
	for k, v := range expected {
		if resp[k] != v {
			t.Errorf("%s: expected %q, got %q", k, v, resp[k])
		}
	}
}

func TestOAuthMetadataHandler_NilMeta(t *testing.T) {
	handler := OAuthMetadataHandler(
		"https://example.com/.well-known/jwks.json",
		"https://example.com/",
		nil,
	)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	if resp["issuer"] != "https://example.com/" {
		t.Errorf("issuer: expected https://example.com/, got %s", resp["issuer"])
	}
	if resp["jwks_uri"] != "https://example.com/.well-known/jwks.json" {
		t.Errorf("jwks_uri: expected https://example.com/.well-known/jwks.json, got %s", resp["jwks_uri"])
	}
}

func TestProtectedResourceMetadataHandler(t *testing.T) {
	handler := ProtectedResourceMetadataHandler(&config.ResourceMetadataConfig{
		Resource:             "https://watchword.example.com",
		AuthorizationServers: []string{"https://auth.example.com"},
	})

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %s", ct)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	if resp["resource"] != "https://watchword.example.com" {
		t.Errorf("resource: expected https://watchword.example.com, got %v", resp["resource"])
	}

	servers, ok := resp["authorization_servers"].([]interface{})
	if !ok || len(servers) != 1 || servers[0] != "https://auth.example.com" {
		t.Errorf("authorization_servers: expected [https://auth.example.com], got %v", resp["authorization_servers"])
	}

	// Default bearer_methods_supported should be ["header"]
	methods, ok := resp["bearer_methods_supported"].([]interface{})
	if !ok || len(methods) != 1 || methods[0] != "header" {
		t.Errorf("bearer_methods_supported: expected [header], got %v", resp["bearer_methods_supported"])
	}
}

func TestProtectedResourceMetadataHandler_AllFields(t *testing.T) {
	handler := ProtectedResourceMetadataHandler(&config.ResourceMetadataConfig{
		Resource:               "https://watchword.example.com",
		AuthorizationServers:   []string{"https://auth1.example.com", "https://auth2.example.com"},
		BearerMethodsSupported: []string{"header", "body"},
		ScopesSupported:        []string{"read", "write"},
	})

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	servers := resp["authorization_servers"].([]interface{})
	if len(servers) != 2 {
		t.Errorf("expected 2 authorization_servers, got %d", len(servers))
	}

	methods := resp["bearer_methods_supported"].([]interface{})
	if len(methods) != 2 || methods[0] != "header" || methods[1] != "body" {
		t.Errorf("bearer_methods_supported: expected [header body], got %v", methods)
	}

	scopes := resp["scopes_supported"].([]interface{})
	if len(scopes) != 2 || scopes[0] != "read" || scopes[1] != "write" {
		t.Errorf("scopes_supported: expected [read write], got %v", scopes)
	}
}
