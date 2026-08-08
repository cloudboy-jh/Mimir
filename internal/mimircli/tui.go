package mimircli

import (
	"context"
	"fmt"
	"os"
	"time"

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
	var agent *pi.Client
	agentStatus := "Mimir assistant unavailable"
	currentModel := ""
	extensionDir, extensionErr := os.MkdirTemp("", "mimir-pi-")
	if extensionErr == nil {
		defer os.RemoveAll(extensionDir)
		executable, executableErr := os.Executable()
		if executableErr == nil {
			extension, writeErr := pi.WriteMimirExtension(extensionDir, executable)
			if writeErr == nil {
				agent, extensionErr = pi.Start(ctx, pi.Config{Dir: ".", Args: []string{
					"--no-session", "--no-extensions", "--no-tools", "--no-skills",
					"--no-prompt-templates", "--no-context-files", "--extension", extension,
					"--system-prompt", mimirtui.SystemPrompt,
				}})
			} else {
				extensionErr = writeErr
			}
		} else {
			extensionErr = executableErr
		}
	}
	if agent != nil {
		handshakeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		state, handshakeErr := agent.GetState(handshakeCtx)
		cancel()
		if handshakeErr != nil {
			closeErr := agent.Close()
			diagnostic := handshakeErr
			if closeErr != nil {
				diagnostic = closeErr
			}
			agentStatus = "Mimir unavailable: " + diagnostic.Error()
			agent = nil
		} else {
			agentStatus = "Mimir ready"
			if state.Model != nil {
				currentModel = state.Model.Provider + "/" + state.Model.ID
			}
		}
	} else if extensionErr != nil {
		agentStatus = "Mimir unavailable: " + extensionErr.Error()
	}

	service := currentSessionService()
	pointer, _ := loadPointer()
	model := mimirtui.New(mimirtui.Options{
		Context:      ctx,
		Out:          out,
		Agent:        tuiAgent(agent),
		AgentStatus:  agentStatus,
		CurrentModel: currentModel,
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

func tuiAgent(client *pi.Client) mimirtui.Agent {
	if client == nil {
		return nil
	}
	return client
}
