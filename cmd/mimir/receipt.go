package main

import (
	"fmt"
	"io"
	"strings"
)

// Receipt is the ephemeral chat projection of a memory-bus event.
// Mark is locked: "◆ mimir"
type Receipt struct {
	Plane   string // control | session | code
	Verb    string
	Subject string
	Meaning string // optional second line
	Status  string // ok | warn | fail
	Metric  string // hash|ms|metric text after status
}

const receiptMark = "◆ mimir"

func (r Receipt) String() string {
	head := fmt.Sprintf("%s  %s.%s", receiptMark, r.Plane, r.Verb)
	if r.Subject != "" {
		head += "  " + r.Subject
	}
	var lines []string
	lines = append(lines, head)
	if r.Meaning != "" {
		lines = append(lines, "         "+r.Meaning)
	}
	if r.Status != "" || r.Metric != "" {
		tail := "         "
		if r.Status != "" {
			tail += r.Status
		}
		if r.Metric != "" {
			if r.Status != "" {
				tail += " · "
			}
			tail += r.Metric
		}
		lines = append(lines, tail)
	}
	return strings.Join(lines, "\n")
}

func writeReceipt(w io.Writer, r Receipt) {
	if w == nil {
		return
	}
	fmt.Fprintln(w, r.String())
}

func failReceipt(plane, verb, reason string) Receipt {
	cfg := mustLoadCfgOrDefault()
	logHint := cfg.Log.Path
	if logHint == "" {
		logHint = "~/.mimir/mimir.log"
	}
	return Receipt{
		Plane:   plane,
		Verb:    verb,
		Subject: "fail",
		Meaning: "reason: " + reason,
		Status:  "fail",
		Metric:  "log: " + logHint,
	}
}
