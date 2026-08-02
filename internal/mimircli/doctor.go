package mimircli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	doctorpkg "github.com/cloudboy-jh/mimir/internal/doctor"
)

func doctor(ctx context.Context, args []string, out io.Writer) error {
	options, err := parseDoctorArgs(args)
	if err != nil {
		return err
	}
	configureInstall()
	service := doctorpkg.New(apiRequester{})
	service.Lifecycle = lifecycleService()
	var report doctorpkg.Report
	if options.tui {
		report = service.RunTUI(ctx)
	} else {
		report = service.Run(ctx)
	}
	if options.json {
		data, err := json.Marshal(report.Structured())
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, string(data))
		return err
	}
	return renderDoctor(out, report)
}

type doctorOptions struct {
	json bool
	tui  bool
}

func parseDoctorArgs(args []string) (doctorOptions, error) {
	var options doctorOptions
	for _, arg := range args {
		switch arg {
		case "--json":
			options.json = true
		case "--tui":
			options.tui = true
		default:
			return doctorOptions{}, fmt.Errorf("usage: mimir doctor [--json] [--tui]")
		}
	}
	return options, nil
}
