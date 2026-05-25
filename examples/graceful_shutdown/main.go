package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/athxx/dax"
)

func main() {
	s := dax.NewServer()
	s.ShowLogo()

	s.Get("/", func(ctx dax.Context) error {
		return ctx.String("Hello")
	})

	s.Get("/slow", func(ctx dax.Context) error {
		// Simulate a long-running request to demonstrate that
		// graceful shutdown waits for in-flight work to finish.
		time.Sleep(10 * time.Second)
		return ctx.String("done")
	})

	// Listen for SIGINT/SIGTERM and trigger graceful shutdown.
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		log.Println("shutdown: waiting for in-flight requests...")
		s.Stop()
	}()

	if err := s.Run(":8080"); err != nil {
		log.Fatal(err)
	}
	log.Println("shutdown: complete")
}
