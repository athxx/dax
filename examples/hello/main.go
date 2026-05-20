package main

import (
	"github.com/athxx/dax"
	"github.com/athxx/dax/midware/cors"
	"github.com/athxx/dax/midware/logger"
	"github.com/athxx/dax/midware/ratelimit"
	"github.com/athxx/dax/midware/recovery"
)

func main() {
	s := dax.NewServer()

	s.Use(recovery.New())
	s.Use(logger.New())
	s.Use(cors.New(cors.Config{AllowedOrigins: []string{"*"}}))
	s.Use(ratelimit.New(ratelimit.Config{Rate: 10, Burst: 5}))

	s.Get("/", func(ctx dax.Context) error {
		return ctx.String("Hello")
	})
	// s.Use(auth.NewJWT(auth.JWTConfig{Secret: "my-secret"}))

	s.Run(":8080")
}
