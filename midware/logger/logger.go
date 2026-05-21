// Package logger provides request logging middleware with configurable format.
//
// Usage:
//
//	s.Use(logger.New(logger.Config{
//	    Format: `[${method}] ${path} ${status} ${duration}`,
//	}))
package logger

import (
	"fmt"
	"time"

	"github.com/athxx/dax"
)

// Config holds the logger configuration.
type Config struct {
	// Format is the log format string.
	// Available placeholders: ${method}, ${path}, ${status}, ${duration}, ${host}, ${query}
	// Default: "[${method}] ${path} ${status} ${duration}"
	Format string
}

var defaultConfig = Config{
	Format: "[${method}] ${path} ${status} ${duration}",
}

// New creates request logging middleware.
func New(config ...Config) dax.Handler {
	cfg := defaultConfig
	if len(config) > 0 {
		cfg = config[0]
	}

	return func(ctx dax.Context) error {
		start := time.Now()

		err := ctx.Next(ctx)

		duration := time.Since(start)
		method := ctx.Request().Method()
		path := ctx.Request().Path()
		status := ctx.Response().Status()

		fmt.Println(formatLog(cfg.Format, method, path, status, duration, ctx))

		return err
	}
}

func formatLog(format, method, path string, status int, duration time.Duration, ctx dax.Context) string {
	// Simple replace-based formatting
	result := format
	result = replaceAll(result, "${method}", method)
	result = replaceAll(result, "${path}", path)
	result = replaceAll(result, "${status}", fmt.Sprintf("%d", status))
	result = replaceAll(result, "${duration}", duration.Round(time.Microsecond).String())
	result = replaceAll(result, "${host}", ctx.Request().Host())
	result = replaceAll(result, "${query}", string(ctx.Request().Query()))
	return result
}

func replaceAll(s, old, new string) string {
	for i := 0; i < len(s); {
		if i+len(old) <= len(s) && s[i:i+len(old)] == old {
			s = s[:i] + new + s[i+len(old):]
			i += len(new)
		} else {
			i++
		}
	}
	return s
}
