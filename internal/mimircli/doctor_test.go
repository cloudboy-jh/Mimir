package mimircli

import (
	"strings"
	"testing"
)

func TestParseDoctorArgsSupportsJSONAndTUI(t *testing.T) {
	options, err := parseDoctorArgs([]string{"--json", "--tui"})
	if err != nil {
		t.Fatal(err)
	}
	if !options.json || !options.tui {
		t.Fatalf("options %#v", options)
	}
}

func TestParseDoctorArgsRejectsUnknownArgumentWithUpdatedUsage(t *testing.T) {
	_, err := parseDoctorArgs([]string{"--unknown"})
	if err == nil || !strings.Contains(err.Error(), "usage: mimir doctor [--json] [--tui]") {
		t.Fatalf("error = %v", err)
	}
}
