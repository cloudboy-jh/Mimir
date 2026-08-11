package mimircli

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	demopkg "github.com/cloudboy-jh/mimir/internal/demo"
)

func demo(ctx context.Context, args []string, ioctx IO) error {
	noOpen := false
	switch {
	case len(args) == 0:
	case len(args) == 1 && args[0] == "--no-open":
		noOpen = true
	default:
		return fmt.Errorf("usage: mimir demo [--no-open]")
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	server, err := demopkg.Start()
	if err != nil {
		return err
	}
	defer server.Close()

	if _, err := fmt.Fprintf(ioctx.Out, "Mimir demo: %s\nSample data only. Changes reset when the page reloads.\nPress Ctrl+C to stop.\n", server.URL()); err != nil {
		return err
	}
	if !noOpen {
		if err := openBrowser(ctx, server.URL()); err != nil {
			if _, writeErr := fmt.Fprintln(ioctx.Out, "Browser did not open automatically. Use the URL above."); writeErr != nil {
				return writeErr
			}
		}
	}
	return server.Serve(ctx)
}
