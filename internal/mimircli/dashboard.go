package mimircli

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"

	cliui "github.com/cloudboy-jh/mimir/internal/ui/appframe"
	"github.com/cloudboy-jh/mimir/internal/ui/bentotui"
)

var openBrowser = func(ctx context.Context, target string) error {
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command, args = "open", []string{target}
	case "windows":
		command, args = "rundll32", []string{"url.dll,FileProtocolHandler", target}
	default:
		command, args = "xdg-open", []string{target}
	}
	process := exec.CommandContext(ctx, command, args...)
	if err := process.Start(); err != nil {
		return err
	}
	go func() { _ = process.Wait() }()
	return nil
}

func dashboard(ctx context.Context, ioctx IO) error {
	pointer, err := loadPointer()
	if err != nil {
		return err
	}
	target := pointer.URL + "/login?returnTo=%2Fdashboard%2Fsessions"
	if err := openBrowser(ctx, target); err != nil {
		render := cliui.New(ioctx.Out)
		_, writeErr := fmt.Fprintln(ioctx.Out, render.Callout(bentotui.ToneWarn, "Could not open the dashboard", target))
		if writeErr != nil {
			return writeErr
		}
		return nil
	}
	render := cliui.New(ioctx.Out)
	_, err = fmt.Fprintln(ioctx.Out, render.Callout(bentotui.ToneSuccess, "Dashboard opened", target))
	return err
}
