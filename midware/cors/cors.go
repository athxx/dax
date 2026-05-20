// Package cors provides Cross-Origin Resource Sharing middleware.
//
// Usage:
//
//	s.Use(cors.New(cors.Config{
//	    AllowedOrigins: []string{"https://example.com"},
//	    AllowedMethods: []string{"GET", "POST"},
//	}))
package cors

import (
	"strings"

	"github.com/athxx/dax"
)

// Config holds the CORS middleware configuration.
type Config struct {
	// AllowedOrigins specifies allowed origins. Use "*" for all.
	AllowedOrigins []string
	// AllowedMethods specifies allowed HTTP methods.
	AllowedMethods []string
	// AllowedHeaders specifies allowed HTTP headers.
	AllowedHeaders []string
	// AllowCredentials indicates whether credentials are allowed.
	AllowCredentials bool
	// MaxAge specifies how long preflight results can be cached.
	MaxAge string
}

var defaultConfig = Config{
	AllowedOrigins: []string{"*"},
	AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
	AllowedHeaders: []string{"Origin", "Content-Type", "Accept", "Authorization"},
	MaxAge:         "86400",
}

// New creates a new CORS middleware.
func New(config ...Config) dax.Handler {
	cfg := defaultConfig
	if len(config) > 0 {
		cfg = config[0]
	}

	allowOrigin := strings.Join(cfg.AllowedOrigins, ",")
	_ = allowOrigin
	allowMethods := strings.Join(cfg.AllowedMethods, ",")
	allowHeaders := strings.Join(cfg.AllowedHeaders, ",")

	return func(ctx dax.Context) error {
		origin := ctx.Request().Header("Origin")
		if origin == "" {
			return ctx.Next(ctx)
		}

		// Check if origin is allowed
		allowed := false
		for _, o := range cfg.AllowedOrigins {
			if o == "*" || o == origin {
				allowed = true
				break
			}
		}

		if !allowed {
			return ctx.Next(ctx)
		}

		// Set CORS headers
		if cfg.AllowedOrigins[0] == "*" && !cfg.AllowCredentials {
			ctx.Response().SetHeader("Access-Control-Allow-Origin", "*")
		} else {
			ctx.Response().SetHeader("Access-Control-Allow-Origin", origin)
		}

		ctx.Response().SetHeader("Access-Control-Allow-Methods", allowMethods)
		ctx.Response().SetHeader("Access-Control-Allow-Headers", allowHeaders)

		if cfg.AllowCredentials {
			ctx.Response().SetHeader("Access-Control-Allow-Credentials", "true")
		}

		if cfg.MaxAge != "" {
			ctx.Response().SetHeader("Access-Control-Max-Age", cfg.MaxAge)
		}

		// Handle preflight
		if ctx.Request().Method() == "OPTIONS" {
			ctx.Response().SetStatus(204)
			return nil
		}

		return ctx.Next(ctx)
	}
}
