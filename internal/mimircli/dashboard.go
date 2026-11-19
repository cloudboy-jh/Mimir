package mimircli

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
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
	target := pointer.URL + "/dashboard/"
	if err := openBrowser(ctx, target); err != nil {
		_, err = fmt.Fprintln(ioctx.Out, "Open "+target)
		return err
	}
	_, err = fmt.Fprintln(ioctx.Out, "Dashboard: "+target)
	return err
}
