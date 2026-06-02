package main

import (
	"context"
	"fmt"
	"os"

	"github.com/cosmtrek/jeju/internal/cli"
)

func main() {
	if err := cli.Execute(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
