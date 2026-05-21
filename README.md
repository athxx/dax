# DaX

A minimal HTTP/1.1 web server designed to sit behind a reverse proxy (caddy, haproxy, nginx) for HTTP/2 and HTTP/3 support.

## Features

- Radix tree router with static, parameterized (`:id`), and wildcard (`*file`) routes
- Middleware chain with automatic 404 bypass — non-existent routes skip all middleware
- Route-registration-order aware middleware — routes registered before `Use()` automatically skip that middleware
- Built-in middlewares: JWT/API-key auth, CORS, rate limiting, recovery, request logging, gzip compression, security hardening, distributed tracing
- Prefork mode via `SO_REUSEPORT` for multi-process load balancing
- Zero-allocation route lookups
- Test-only synthetic requests via `Request()`

## Installation

```shell
go get github.com/athxx/dax
```

## Usage

```go
s := dax.NewServer()

s.Get("/", func(ctx dax.Context) error {
    return ctx.String("Hello")
})

s.Get("/blog/:post", func(ctx dax.Context) error {
    return ctx.String(ctx.Request().Param("post"))
})

s.Get("/images/*file", func(ctx dax.Context) error {
    return ctx.String(ctx.Request().Param("file"))
})

if err := s.Run(":8080"); err != nil {
    panic(err)
}
```

### Middleware

```go
// Public route — registered before JWT, so auth is skipped automatically
s.Get("/health", healthHandler)

// Protect everything registered after this point
s.Use(auth.NewJWT(auth.JWTConfig{Secret: "my-secret"}))
s.Get("/admin", adminHandler)
```

Use `ctx.Next(ctx)` to pass control to the next handler.

### Built-in middlewares

| Package   | Description                    |
| --------- | ------------------------------ |
| auth      | JWT and API-key authentication |
| cors      | Cross-origin resource sharing  |
| ratelimit | Token-bucket rate limiter      |
| recovery  | Panic recovery                 |
| logger    | Request logging                |
| gzip      | Response compression           |
| security  | Basic security hardening       |
| tracing   | Distributed tracing IDs        |

## License

MIT
