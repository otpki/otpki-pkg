package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/otpki/otpki-pkg/tokenauth"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	src, err := tokenauth.BuildTokenSource(ctx, tokenauth.DefaultConfig())
	if err != nil {
		fatal(err)
	}
	tok, err := src.Token()
	if err != nil {
		fatal(err)
	}
	fmt.Println(tok.AccessToken)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
