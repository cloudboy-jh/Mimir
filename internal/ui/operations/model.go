package operations

import (
	"fmt"
	"time"
)

type StepState string

const (
	StepPending  StepState = "pending"
	StepActive   StepState = "active"
	StepComplete StepState = "complete"
	StepFailed   StepState = "failed"
)

func formatElapsed(value time.Duration) string {
	if value < time.Second {
		return fmt.Sprintf("%dms", value.Milliseconds())
	}
	return value.Round(100 * time.Millisecond).String()
}
