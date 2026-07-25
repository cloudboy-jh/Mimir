package mimircli

import (
	"context"
	"time"

	"github.com/cloudboy-jh/mimir/internal/sessions"
)

var sessionStatusPollScheduleOverride []time.Duration

type apiRequester struct{}

func (apiRequester) Request(ctx context.Context, method, path string, body any) ([]byte, error) {
	return remoteRequest(ctx, method, path, body)
}

func currentSessionService() sessions.Service {
	service := sessions.New(apiRequester{})
	if sessionStatusPollScheduleOverride != nil {
		service.PollSchedule = sessionStatusPollScheduleOverride
	}
	return service
}
