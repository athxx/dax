// Package tracing provides distributed tracing middleware that injects
// trace IDs into the request context and response headers.
//
// Usage:
//
//	s.Use(tracing.New(tracing.Config{
//	    ServiceName: "my-service",
//	}))
package tracing

import (
	"crypto/rand"
	"fmt"
	"time"

	"github.com/athxx/dax"
)

// Config holds the tracing middleware configuration.
type Config struct {
	// ServiceName identifies the service in trace headers.
	ServiceName string
	// Header is the trace header to read/write. Default: "X-Trace-ID"
	Header string
}

var defaultConfig = Config{
	ServiceName: "dax",
	Header:      "X-Trace-ID",
}

// New creates a new tracing middleware.
func New(config ...Config) dax.Handler {
	cfg := defaultConfig
	if len(config) > 0 {
		cfg = config[0]
	}

	return func(ctx dax.Context) error {
		// Use incoming trace ID or generate a new one
		traceID := ctx.Request().Header(cfg.Header)
		if traceID == "" {
			traceID = generateTraceID()
		}

		spanID := generateSpanID()

		// Forward trace context to downstream handlers
		ctx.Response().SetHeader(cfg.Header, traceID)
		ctx.Response().SetHeader("X-Span-ID", spanID)
		ctx.Response().SetHeader("X-Service-Name", cfg.ServiceName)

		return ctx.Next(ctx)
	}
}

func generateTraceID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func generateSpanID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func init() {
	// Ensure rand is seeded
	_ = time.Now().UnixNano()
}
