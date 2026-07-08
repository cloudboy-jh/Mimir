package main

import (
	"context"
	"os"

	"github.com/cloudboy-jh/churn/internal/cli"
)

func main() {
	if err := cli.Execute(context.Background(), os.Args[1:]); err != nil {
		os.Exit(1)
	}
}
