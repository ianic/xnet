package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/ianic/ws"
)

// InteruptContext returns context which will be closed on application interupt
func InteruptContext() context.Context {
	ctx, stop := context.WithCancel(context.Background())
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
		<-c
		stop()
	}()
	return ctx
}

func main() {
	address := "localhost:9001"

	ctx := InteruptContext()
	if err := ws.Serve(ctx, address, ws.Echo); err != nil {
		log.Fatal(err)
	}
}
