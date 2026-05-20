package main

import "github.com/athxx/dax"

func main() {
	s := dax.NewServer()

	s.Get("/", func(ctx dax.Context) error {
		return ctx.String("Hello")
	})

	s.Run(":8080")
}
