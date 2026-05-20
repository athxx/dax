// Package gzip provides response compression middleware using gzip.
//
// Usage:
//
//	s.Use(gzip.New(gzip.Config{Level: 6}))
package gzip

import (
	"bytes"
	"compress/gzip"
	"strings"

	"github.com/athxx/dax"
)

// Config holds the gzip middleware configuration.
type Config struct {
	// Level is the compression level (1-9, 0 for default).
	Level int
	// MinLength is the minimum response body length to compress. Default: 1024 bytes.
	MinLength int
}

var defaultConfig = Config{
	Level:     6,
	MinLength: 1024,
}

// New creates a new gzip compression middleware.
func New(config ...Config) dax.Handler {
	cfg := defaultConfig
	if len(config) > 0 {
		cfg = config[0]
	}

	return func(ctx dax.Context) error {
		// Check if client accepts gzip
		acceptEncoding := ctx.Request().Header("Accept-Encoding")
		if !strings.Contains(acceptEncoding, "gzip") {
			return ctx.Next(ctx)
		}

		err := ctx.Next(ctx)
		if err != nil {
			return err
		}

		body := ctx.Response().Body()
		if len(body) < cfg.MinLength {
			return nil
		}

		var buf bytes.Buffer
		gw, _ := gzip.NewWriterLevel(&buf, cfg.Level)
		_, _ = gw.Write(body)
		gw.Close()

		ctx.Response().SetBody(buf.Bytes())
		ctx.Response().SetHeader("Content-Encoding", "gzip")
		ctx.Response().DeleteHeader("Content-Length")

		return nil
	}
}
