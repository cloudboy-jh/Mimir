package main

import (
	"context"
	"fmt"
	"os"
)

func main() {
	if err := Execute(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
