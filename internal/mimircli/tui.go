package mimircli

import (
	"context"
	"fmt"
	"os"
	"strings"

	doctorpkg "github.com/cloudboy-jh/mimir/internal/doctor"
	"github.com/cloudboy-jh/mimir/internal/pi"
	"github.com/cloudboy-jh/mimir/internal/sessions"
	"github.com/cloudboy-jh/mimir/internal/ui/appframe"
	"github.com/cloudboy-jh/mimir/internal/ui/bentotui"
	mimirtui "github.com/cloudboy-jh/mimir/internal/ui/mimirtui"
	sessionui "github.com/cloudboy-jh/mimir/internal/ui/sessions"
)

func cmdTUI(ctx context.Context, args []string, ioctx IO) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: mimir tui")
	}
	in, inputOK := ioctx.In.(*os.File)
	out, outputOK := ioctx.Out.(*os.File)
	if !inputOK || !outputOK || !appframe.Interactive(in, out) {
		return fmt.Errorf("mimir tui requires an interactive terminal of at least 48x12")
	}
	readiness := doctorpkg.New(apiRequester{}).TUIReadiness()
	if !readiness.OK {
		problems := make([]string, 0, len(readiness.Checks))
		for _, check := range readiness.Checks {
			if check.Status == "failed" {
				problems = append(problems, check.Detail+"; "+check.Repair)
			}
		}
		return fmt.Errorf("TUI prerequisites failed: %s", strings.Join(problems, "; "))
	}

	extensionDir, err := os.MkdirTemp("", "mimir-pi-")
	if err != nil {
		return fmt.Errorf("preparing Pi tools: %w", err)
	}
	defer os.RemoveAll(extensionDir)
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating Mimir executable: %w", err)
	}
	extension, err := pi.WriteMimirExtension(extensionDir, executable)
	if err != nil {
		return err
	}
	agent, err := pi.Start(ctx, pi.Config{Dir: ".", Args: []string{"--no-extensions", "--no-builtin-tools", "--extension", extension}})
	if err != nil {
		return fmt.Errorf("starting terminal agent: %w; run `mimir doctor --tui`", err)
	}

	service := currentSessionService()
	pointer, _ := loadPointer()
	model := mimirtui.New(mimirtui.Options{
		Context: ctx,
		Out:     out,
		Agent:   agent,
		Load: func(ctx context.Context) ([]sessionui.BrowserSession, error) {
			values, err := service.FetchReceipts(ctx, "", "")
			if err != nil {
				return nil, err
			}
			return sessionui.Items(values, pointer.URL, 100), nil
		},
		GetDetail: service.Get,
		SetOutcome: func(ctx context.Context, id string, options sessions.SetOutcomeOptions) error {
			_, err := service.SetOutcome(ctx, id, options)
			return err
		},
	})
	defer model.Close()
	return bentotui.RunWithOptions(ctx, in, out, model, bentotui.RunOptions{AlternateScreen: true, Mouse: true})
}
