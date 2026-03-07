package auth

import (
	"context"
	"fmt"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"

	"github.com/watchword/watchword/internal/config"
)

type JWTValidator struct {
	jwks   keyfunc.Keyfunc
	parser *jwt.Parser
	cancel context.CancelFunc
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

	return &JWTValidator{
		jwks:   jwks,
		parser: jwt.NewParser(opts...),
		cancel: cancel,
	}, nil
}

func (v *JWTValidator) Validate(tokenStr string) error {
	_, err := v.parser.Parse(tokenStr, v.jwks.KeyfuncCtx(context.Background()))
	if err != nil {
		return fmt.Errorf("jwt validation failed: %w", err)
	}
	return nil
}

func (v *JWTValidator) Close() {
	v.cancel()
}
