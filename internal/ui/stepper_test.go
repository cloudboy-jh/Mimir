package ui

import (
	"bytes"
	"strings"
	"testing"
)

func TestStepperPrintsStableRedirectedOutput(t *testing.T) {
	var out bytes.Buffer
	stepper := StartStepper(&out, "Mimir setup", []string{"Preparing", "Deploying"})
	stepper.Complete("Worker prepared")
	stepper.FailCurrent()
	stepper.Stop()
	got := out.String()
	for _, want := range []string{"◆ Mimir setup", "[✓] Worker prepared", "[×] Deploying"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output %q does not contain %q", got, want)
		}
	}
	if strings.Contains(got, "\x1b[") || strings.Contains(got, "\r") {
		t.Fatalf("redirected output contains terminal controls: %q", got)
	}
}
