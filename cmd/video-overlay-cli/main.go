package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"

	"video-overlay-cli/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		stop()
	}()
	if err := cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, context.Canceled) {
			if err == context.Canceled {
				fmt.Fprintln(os.Stderr, "Interrupted")
			} else {
				fmt.Fprintln(os.Stderr, err)
			}
			os.Exit(130)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
