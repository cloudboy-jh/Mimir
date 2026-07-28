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
	report := service.Run(ctx)
	if jsonOutput {
		data, err := json.Marshal(report.Structured())
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, string(data))
		return err
	}
	return renderDoctor(out, report)
}
