package main

import (
	"bytes"
	"testing"
)

func TestSetupProgressStopIsIdempotent(t *testing.T) {
	var output bytes.Buffer
	progress := &setupProgress{out: &output, enabled: true, phases: []string{"testing"}}
	progress.Resume()
	progress.Stop()
	first := output.String()
	progress.Stop()
	if output.String() != first {
		t.Fatal("second stop wrote additional output")
	}
}
