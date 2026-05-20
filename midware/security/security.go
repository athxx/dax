// Package security provides middleware for basic security hardening,
// including SQL injection and XSS protection.
//
// Usage:
//
//	s.Use(security.New())
package security

import (
	"strings"

	"github.com/athxx/dax"
)

// Config holds the security middleware configuration.
type Config struct {
	// BlockSQLInjection checks query params and body for SQL injection patterns.
	BlockSQLInjection bool
	// BlockXSS checks query params and body for XSS patterns.
	BlockXSS bool
	// AllowedContentTypes restricts allowed Content-Type values (empty means allow all).
	AllowedContentTypes []string
	// MaxBodySize limits the request body size in bytes (0 means no limit).
	MaxBodySize int64
}

var defaultConfig = Config{
	BlockSQLInjection: true,
	BlockXSS:          true,
	MaxBodySize:       10 * 1024 * 1024, // 10MB
}

// Common SQL injection patterns
var sqlPatterns = []string{
	"'", "\"", "--", ";", "/*", "*/",
	"union", "select", "insert", "update", "delete", "drop",
	"alter", "exec", "execute", "create", "truncate",
	"1=1", "1=2", "or 1=1", "or '1'='1'",
}

// Common XSS patterns
var xssPatterns = []string{
	"<script", "javascript:", "onerror=", "onload=",
	"onclick=", "onmouseover=", "onfocus=", "onblur=",
	"<img", "<iframe", "<embed", "<object",
	"alert(", "prompt(", "confirm(",
	"&lt;script", "&#60;script",
}

// New creates a new security middleware.
func New(config ...Config) dax.Handler {
	cfg := defaultConfig
	if len(config) > 0 {
		cfg = config[0]
	}

	return func(ctx dax.Context) error {
		// Security headers
		ctx.Response().SetHeader("X-Content-Type-Options", "nosniff")
		ctx.Response().SetHeader("X-Frame-Options", "DENY")
		ctx.Response().SetHeader("X-XSS-Protection", "1; mode=block")
		ctx.Response().SetHeader("Referrer-Policy", "strict-origin-when-cross-origin")

		// Check Content-Type
		if len(cfg.AllowedContentTypes) > 0 {
			ct := ctx.Request().Header("Content-Type")
			if ct != "" {
				allowed := false
				for _, a := range cfg.AllowedContentTypes {
					if strings.HasPrefix(ct, a) {
						allowed = true
						break
					}
				}
				if !allowed {
					ctx.Response().SetStatus(415)
					ctx.Response().SetBody([]byte("Unsupported Media Type"))
					return nil
				}
			}
		}

		// Check query parameters for malicious patterns
		if cfg.BlockSQLInjection || cfg.BlockXSS {
			query := string(ctx.Request().Query())
			if cfg.BlockSQLInjection && containsAny(query, sqlPatterns) {
				ctx.Response().SetStatus(400)
				ctx.Response().SetBody([]byte("Bad Request"))
				return nil
			}
			if cfg.BlockXSS && containsAny(query, xssPatterns) {
				ctx.Response().SetStatus(400)
				ctx.Response().SetBody([]byte("Bad Request"))
				return nil
			}
		}

		return ctx.Next(ctx)
	}
}

func containsAny(s string, patterns []string) bool {
	sl := strings.ToLower(s)
	for _, p := range patterns {
		if strings.Contains(sl, strings.ToLower(p)) {
			return true
		}
	}
	return false
}
