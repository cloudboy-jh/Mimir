package mimircli

import (
	"strings"
	"testing"
)

func TestParseDoctorArgsSupportsJSON(t *testing.T) {
	jsonOutput, err := parseDoctorArgs([]string{"--json"})
	if err != nil {
		t.Fatal(err)
	}
	if !jsonOutput {
		t.Fatal("JSON output was not enabled")
	}
}

func TestParseDoctorArgsRejectsUnknownArgumentWithUpdatedUsage(t *testing.T) {
	_, err := parseDoctorArgs([]string{"--tui"})
	if err == nil || !strings.Contains(err.Error(), "usage: mimir doctor [--json]") {
		t.Fatalf("error = %v", err)
	}
}
