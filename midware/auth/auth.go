// Package auth provides JWT and API-key authentication middleware.
//
// Usage:
//
//	s.Use(auth.NewJWT(auth.JWTConfig{Secret: "my-secret"}))
//	s.Use(auth.NewAPIKey(auth.APIKeyConfig{Keys: []string{"sk-123", "sk-456"}}))
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/athxx/dax"
)

// -------------------- JWT Middleware --------------------

// JWTConfig holds the JWT authentication configuration.
type JWTConfig struct {
	// Secret is the HMAC-SHA256 signing key.
	Secret string
	// TokenFromHeader is the header to extract the token from. Default: "Authorization"
	TokenFromHeader string
	// TokenPrefix is the prefix before the token value. Default: "Bearer "
	TokenPrefix string
	// Skip paths that don't require authentication.
	Skip []string
}

var defaultJWTConfig = JWTConfig{
	TokenFromHeader: "Authorization",
	TokenPrefix:     "Bearer ",
}

// NewJWT creates JWT middleware. Tokens use HMAC-SHA256.
func NewJWT(config JWTConfig) dax.Handler {
	if config.Secret == "" {
		panic("auth: JWT secret is required")
	}
	if config.TokenFromHeader == "" {
		config.TokenFromHeader = defaultJWTConfig.TokenFromHeader
	}
	if config.TokenPrefix == "" {
		config.TokenPrefix = defaultJWTConfig.TokenPrefix
	}

	return func(ctx dax.Context) error {
		// Skip paths
		path := ctx.Request().Path()
		for _, s := range config.Skip {
			if strings.HasPrefix(path, s) {
				return ctx.Next(ctx)
			}
		}

		authHeader := ctx.Request().Header(config.TokenFromHeader)
		if authHeader == "" || !strings.HasPrefix(authHeader, config.TokenPrefix) {
			ctx.Response().SetHeader("WWW-Authenticate", "Bearer")
			ctx.Response().SetStatus(401)
			ctx.Response().SetBody([]byte("Unauthorized"))
			return nil
		}

		token := strings.TrimPrefix(authHeader, config.TokenPrefix)
		claims, err := verifyJWT(token, config.Secret)
		if err != nil {
			ctx.Response().SetStatus(401)
			ctx.Response().SetBody([]byte("Invalid or expired token"))
			return nil
		}

		// Store claims in a header so downstream handlers can read them
		ctx.Response().SetHeader("X-Claims-Sub", fmt.Sprintf("%v", claims["sub"]))
		_ = claims

		return ctx.Next(ctx)
	}
}

// verifyJWT validates an HMAC-SHA256 JWT token and returns its claims.
func verifyJWT(token, secret string) (map[string]interface{}, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid token format")
	}

	// Verify signature
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	if expectedSig != parts[2] {
		return nil, fmt.Errorf("invalid signature")
	}

	// Decode payload
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid payload encoding: %w", err)
	}

	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("invalid claims: %w", err)
	}

	// Check expiration
	if exp, ok := claims["exp"]; ok {
		expF, _ := exp.(float64)
		if time.Now().Unix() > int64(expF) {
			return nil, fmt.Errorf("token expired")
		}
	}

	return claims, nil
}

// -------------------- API Key Middleware --------------------

// APIKeyConfig holds the API key authentication configuration.
type APIKeyConfig struct {
	// Keys is the list of valid API keys.
	Keys []string
	// Header is the header to extract the API key from. Default: "X-API-Key"
	Header string
	// Skip paths that don't require authentication.
	Skip []string
}

var defaultAPIKeyConfig = APIKeyConfig{
	Header: "X-API-Key",
}

// NewAPIKey creates API key middleware.
func NewAPIKey(config APIKeyConfig) dax.Handler {
	if len(config.Keys) == 0 {
		panic("auth: at least one API key is required")
	}
	if config.Header == "" {
		config.Header = defaultAPIKeyConfig.Header
	}

	keySet := make(map[string]struct{}, len(config.Keys))
	for _, k := range config.Keys {
		keySet[k] = struct{}{}
	}

	return func(ctx dax.Context) error {
		path := ctx.Request().Path()
		for _, s := range config.Skip {
			if strings.HasPrefix(path, s) {
				return ctx.Next(ctx)
			}
		}

		key := ctx.Request().Header(config.Header)
		if key == "" {
			ctx.Response().SetStatus(401)
			ctx.Response().SetBody([]byte("Missing API key"))
			return nil
		}

		if _, ok := keySet[key]; !ok {
			ctx.Response().SetStatus(403)
			ctx.Response().SetBody([]byte("Invalid API key"))
			return nil
		}

		return ctx.Next(ctx)
	}
}
