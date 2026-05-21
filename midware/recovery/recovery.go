// Package recovery provides panic recovery middleware.
//
// Usage:
//
//	s.Use(recovery.New())
package recovery

import (
	"fmt"
	"log"
	"runtime"

	"github.com/athxx/dax"
)

// Config holds the recovery middleware configuration.
type Config struct {
	// PrintStack prints the full stack trace when a panic occurs.
	PrintStack bool
}

var defaultConfig = Config{
	PrintStack: false,
}

// New creates panic recovery middleware.
func New(config ...Config) dax.Handler {
	cfg := defaultConfig
	if len(config) > 0 {
		cfg = config[0]
	}

	return func(ctx dax.Context) (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic recovered: %v", r)

				if cfg.PrintStack {
					buf := make([]byte, 4096)
					n := runtime.Stack(buf, false)
					log.Printf("panic stack trace:\n%s", buf[:n])
				}

				ctx.Response().SetStatus(500)
				ctx.Response().SetBody([]byte("Internal Server Error"))
			}
		}()

		return ctx.Next(ctx)
	}
}
