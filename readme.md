# web

A minimal HTTP/1.1 web server that sits behind a reverse proxy like `caddy`, `haproxy` or `nginx` for HTTP 1/2/3 support.

## Features

- Low latency
- High throughput
- Scales incredibly well

## Installation

```shell
go get git.urbach.dev/go/web
```

## Usage

### Quick start

```go
s :=daxNewServer()
// add routes here
s.Run(":8080")
```

### Static route

```go
s.Get("/", func(ctxdaxContext) error {
	return ctx.String("Hello")
})
```

### Parameter route

```go
s.Get("/blog/:post", func(ctxdaxContext) error {
	post := ctx.Request().Param("post")
	return ctx.String(post)
})
```

### Wildcard route

```go
s.Get("/images/*file", func(ctxdaxContext) error {
	file := ctx.Request().Param("file")
	return ctx.String(file)
})
```

### REST methods

```go
s.Post("/api/user/:id", func(ctxdaxContext) error {
	id := ctx.Request().Param("id")
	return ctx.String(id)
})
```

### Middleware

#### Registration

```go
// Single function
s.Use(middleware)

// Multiple functions
s.Use(middleware1, middleware2, middleware3)
```

#### Example: Response time logging

```go
func ResponseTime(ctxdaxContext) error {
	start := time.Now()

	defer func() {
		fmt.Println(ctx.Request().Path(), time.Since(start))
	}()

	return ctx.Next(ctx)
}
```

#### Example: Recover from panics

```go
func Recover(ctxdaxContext) error {
	defer func() {
		err := recover()

		if err != nil {
			fmt.Println(err)
		}
	}()

	return ctx.Next(ctx)
}
```

#### Example: Custom contexts

```go
type Custom struct {
	dax.Context
	User string
}

s.Use(func(ctxdaxContext) error {
	custom := &Custom{Context: ctx, User: "admin"}
	return ctx.Next(custom)
})

s.Get("/", func(ctxdaxContext) error {
	custom := ctx.(*Custom)
	return ctx.String(ctx.User)
})
```

### Query parameters

```go
s.Get("/search", func(ctxdaxContext) error {
	term := ctx.Query().Param("term")
	return ctx.String(term)
})
```

### Custom 404 page

```go
s.All("/*", func(ctxdaxContext) error {
	return ctx.Status(404).String("Not found")
})
```

## Tests

```
PASS: TestBytes
PASS: TestString
PASS: TestError
PASS: TestErrorMultiple
PASS: TestRedirect
PASS: TestQueryParam
PASS: TestQueryParamDuplicate
PASS: TestQueryParamEmpty
PASS: TestQueryParamWithSpace
PASS: TestRequest
PASS: TestRequestBody
PASS: TestRequestBodyMissingLength
PASS: TestRequestHeader
PASS: TestRequestMethods
PASS: TestRequestMethodsAll
PASS: TestRequestParam
PASS: TestWrite
PASS: TestWriteString
PASS: TestResponseCompression
PASS: TestResponseHeader
PASS: TestResponseHeaderOverwrite
PASS: TestPanic
PASS: TestRun
PASS: TestBadRequest
PASS: TestBadRequestHeader
PASS: TestBadRequestMethod
PASS: TestBadRequestPath
PASS: TestBadRequestProtocol
PASS: TestConnectionClose
PASS: TestEarlyClose
PASS: TestSetErrorHandler
PASS: TestUnavailablePort
coverage: 100.0% of statements
```

## Benchmarks

![wrk Benchmark](https://i.imgur.com/6cDeZVA.png)

## Contributors

- [Victor A Higuita](https://git.urbach.dev/vickodev): https://git.urbach.dev/go/web/pulls/1

## License

Please see the [license documentation](https://urbach.dev/license).

## Copyright

© 2024 Eduard Urbach
