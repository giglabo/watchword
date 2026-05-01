package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"

	"github.com/watchword/watchword/internal/config"
)

type JWTValidator struct {
	jwks           keyfunc.Keyfunc
	parser         *jwt.Parser
	requiredScopes []string
	identityClaim  string
	cancel         context.CancelFunc
}

func NewJWTValidator(ctx context.Context, cfg *config.JWTConfig) (*JWTValidator, error) {
	jwksCtx, cancel := context.WithCancel(ctx)

	jwks, err := keyfunc.NewDefaultCtx(jwksCtx, []string{cfg.JWKSURL})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("creating JWKS keyfunc: %w", err)
	}

	opts := []jwt.ParserOption{
		jwt.WithValidMethods([]string{"RS256", "RS384", "RS512", "ES256", "ES384", "ES512"}),
	}
	if cfg.Issuer != "" {
		opts = append(opts, jwt.WithIssuer(cfg.Issuer))
	}
	if cfg.Audience != "" {
		opts = append(opts, jwt.WithAudience(cfg.Audience))
	}

	identityClaim := cfg.IdentityClaim
	if identityClaim == "" {
		identityClaim = "sub"
	}

	return &JWTValidator{
		jwks:           jwks,
		parser:         jwt.NewParser(opts...),
		requiredScopes: cfg.RequiredScopes,
		identityClaim:  identityClaim,
		cancel:         cancel,
	}, nil
}

// Validate verifies the token's signature, issuer, audience and required
// scopes. On success it returns the caller's identity, read from the
// configured claim (default `sub`). Identity is "" when the claim is missing.
func (v *JWTValidator) Validate(tokenStr string) (string, error) {
	token, err := v.parser.Parse(tokenStr, v.jwks.KeyfuncCtx(context.Background()))
	if err != nil {
		return "", fmt.Errorf("jwt validation failed: %w", err)
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", fmt.Errorf("jwt validation failed: unexpected claims type")
	}
	if len(v.requiredScopes) > 0 {
		if err := checkScopes(claims, v.requiredScopes); err != nil {
			return "", fmt.Errorf("jwt validation failed: %w", err)
		}
	}
	identity, _ := claims[v.identityClaim].(string)
	return identity, nil
}

// checkScopes ensures every required scope is present in the token's
// `scope` (space-delimited string, used by Auth0 and Keycloak) or `scp`
// (array, used by some other IdPs) claim.
func checkScopes(claims jwt.MapClaims, required []string) error {
	granted := map[string]struct{}{}
	switch s := claims["scope"].(type) {
	case string:
		for _, p := range strings.Fields(s) {
			granted[p] = struct{}{}
		}
	}
	if arr, ok := claims["scp"].([]any); ok {
		for _, item := range arr {
			if s, ok := item.(string); ok {
				granted[s] = struct{}{}
			}
		}
	}
	for _, want := range required {
		if _, ok := granted[want]; !ok {
			return fmt.Errorf("missing required scope %q", want)
		}
	}
	return nil
}

func (v *JWTValidator) Close() {
	v.cancel()
}
