package sessions

import "context"

type Requester interface {
	Request(context.Context, string, string, any) ([]byte, error)
}

type Service struct {
	API Requester
}

func New(api Requester) Service {
	return Service{API: api}
}
