package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/cloudboy-jh/mimir/internal/deployment"
	"github.com/cloudboy-jh/mimir/internal/mimircli"
)

var (
	version = "0.0.0-dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	mimircli.SetBuildInfo(version, commit, date)
	if code := run(context.Background(), os.Args[1:], os.Stderr); code != mimircli.ExitSuccess {
		os.Exit(code)
	}
}

var execute = mimircli.Execute

func run(ctx context.Context, args []string, stderr io.Writer) int {
	err := execute(ctx, args)
	if err == nil {
		return mimircli.ExitSuccess
	}
	code := mimircli.ExitCode(err)
	if hasJSONFlag(args) {
		var state deployment.StateError
		if errors.As(err, &state) {
			_ = json.NewEncoder(stderr).Encode(struct {
				deployment.StateError
				ExitCode int `json:"exit_code"`
			}{StateError: state, ExitCode: code})
		} else {
			_ = json.NewEncoder(stderr).Encode(map[string]any{"error": err.Error(), "exit_code": code})
		}
	} else {
		var state deployment.StateError
		if errors.As(err, &state) {
			fmt.Fprintln(stderr, state.Message)
		} else {
			fmt.Fprintln(stderr, err)
		}
	}
	return code
}

func hasJSONFlag(args []string) bool {
	for _, arg := range args {
		if arg == "--json" {
			return true
		}
	}
	return false
}
