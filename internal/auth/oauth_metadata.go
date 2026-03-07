package auth

import (
	"encoding/json"
	"net/http"

	"github.com/watchword/watchword/internal/config"
)

// oauthMetadata is the response for /.well-known/oauth-authorization-server (RFC 8414).
type oauthMetadata struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint,omitempty"`
	TokenEndpoint         string `json:"token_endpoint,omitempty"`
	JWKSURI               string `json:"jwks_uri"`
}

// OAuthMetadataHandler serves the Authorization Server Metadata document.
// This is typically used when Watchword acts as both resource and auth server
// (legacy 2025-03-26 MCP spec compatibility).
func OAuthMetadataHandler(jwksURL, issuer string, meta *config.OAuthMetadataConfig) http.HandlerFunc {
	resp := oauthMetadata{
		Issuer:  issuer,
		JWKSURI: jwksURL,
	}
	if meta != nil {
		resp.AuthorizationEndpoint = meta.AuthorizationEndpoint
		resp.TokenEndpoint = meta.TokenEndpoint
	}

	body, _ := json.Marshal(resp)

	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}
}

// protectedResourceMetadata is the response for /.well-known/oauth-protected-resource (RFC 9728).
type protectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	BearerMethodsSupported []string `json:"bearer_methods_supported,omitempty"`
	ScopesSupported        []string `json:"scopes_supported,omitempty"`
}

// ProtectedResourceMetadataHandler serves the Protected Resource Metadata document (RFC 9728).
// This tells MCP clients which authorization server to use.
func ProtectedResourceMetadataHandler(cfg *config.ResourceMetadataConfig) http.HandlerFunc {
	resp := protectedResourceMetadata{
		Resource:             cfg.Resource,
		AuthorizationServers: cfg.AuthorizationServers,
	}
	if len(cfg.BearerMethodsSupported) > 0 {
		resp.BearerMethodsSupported = cfg.BearerMethodsSupported
	} else {
		resp.BearerMethodsSupported = []string{"header"}
	}
	if len(cfg.ScopesSupported) > 0 {
		resp.ScopesSupported = cfg.ScopesSupported
	}

	body, _ := json.Marshal(resp)

	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}
}
