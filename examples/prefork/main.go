package main

import (
	"github.com/athxx/dax"
)

type Custom struct {
	dax.Context
	User string
}

func main() {
	s := dax.NewServer()
	s.Prefork() // Enable prefork mode

	s.Use(func(ctx dax.Context) error {
		custom := &Custom{Context: ctx, User: "admin"}
		return ctx.Next(custom)
	})

	s.Get("/", func(ctx dax.Context) error {
		custom := ctx.(*Custom)
		return ctx.String(custom.User)
	})

	s.Run(":8080")
}
