package mimircli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	doctorpkg "github.com/cloudboy-jh/mimir/internal/doctor"
)

func doctor(ctx context.Context, args []string, out io.Writer) error {
	jsonOutput := false
	for _, arg := range args {
		if arg != "--json" {
			return fmt.Errorf("usage: mimir doctor [--json]")
		}
		jsonOutput = true
	}
	configureInstall()
	service := doctorpkg.New(apiRequester{})
	service.Lifecycle = lifecycleService()
	service.FindOpenCode = func() (string, error) { return findOpenCode() }
	report := service.Run(ctx)
	if jsonOutput {
		data, err := json.Marshal(report)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, string(data))
		return err
	}
	for _, check := range report.Checks {
		fmt.Fprintf(out, "%s  %s", check.Status, check.Name)
		if check.Detail != "" {
			fmt.Fprintf(out, " · %s", check.Detail)
		}
		if check.Repair != "" {
			fmt.Fprintf(out, " · repair: %s", check.Repair)
		}
		fmt.Fprintln(out)
	}
	return nil
}
