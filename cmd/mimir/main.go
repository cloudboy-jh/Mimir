package main

import (
	"context"
	"fmt"
	"os"

	"github.com/cloudboy-jh/mimir/internal/mimircli"
)

var (
	version = "0.0.0-dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	mimircli.SetBuildInfo(version, commit, date)
	if err := mimircli.Execute(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
