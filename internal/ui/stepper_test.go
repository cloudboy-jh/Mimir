package ui

import (
	"bytes"
	"strings"
	"sync"
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
	steps := stepper.Steps()
	if len(steps) != 2 || steps[0].State != StepComplete || steps[1].State != StepFailed {
		t.Fatalf("steps = %#v", steps)
	}
}

func TestStepperSerializesConcurrentTransitions(t *testing.T) {
	var out bytes.Buffer
	stepper := StartStepper(&out, "Mimir setup", []string{"Preparing", "Deploying"})
	var group sync.WaitGroup
	for _, label := range []string{"First complete", "Second complete"} {
		group.Add(1)
		go func(label string) {
			defer group.Done()
			stepper.Complete(label)
		}(label)
	}
	group.Wait()
	steps := stepper.Steps()
	if len(steps) != 2 || steps[0].State != StepComplete || steps[1].State != StepComplete {
		t.Fatalf("steps = %#v", steps)
	}
	if strings.Count(out.String(), "[✓]") != 2 {
		t.Fatalf("output = %q", out.String())
	}
}
