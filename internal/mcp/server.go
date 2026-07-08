package mcp

import (
	"context"
	"fmt"
	"io"
)

type Options struct {
	Dir string
	In  io.Reader
	Out io.Writer
}

func Serve(ctx context.Context, opts Options) error {
	_, _ = ctx, opts.In
	_, err := fmt.Fprintln(opts.Out, "churn MCP stdio server scaffold is wired; tool implementation starts in phase 8")
	return err
}
