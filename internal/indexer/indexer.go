package indexer

import (
	"context"
	"fmt"

	churngit "github.com/cloudboy-jh/churn/internal/git"
)

type Options struct {
	Dir  string
	Full bool
}

type Result struct {
	Message string
}

func Run(ctx context.Context, opts Options) (Result, error) {
	info, err := churngit.Detect(ctx, opts.Dir)
	if err != nil {
		return Result{}, err
	}

	mode := "incremental"
	if opts.Full || !info.StoreExists {
		mode = "full"
	}

	return Result{Message: fmt.Sprintf("index %s queued for %s; durable store implementation starts in phase 4", mode, info.Root)}, nil
}
