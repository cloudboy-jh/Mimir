package recall

import (
	"context"
	"encoding/json"
	"fmt"

	churngit "github.com/cloudboy-jh/churn/internal/git"
)

type Options struct {
	Dir    string
	Query  string
	Budget int
	JSON   bool
}

type Result struct {
	Output string
}

type response struct {
	Query  string `json:"query"`
	Budget int    `json:"budget"`
	Stale  bool   `json:"stale"`
	Note   string `json:"note"`
}

func Run(ctx context.Context, opts Options) (Result, error) {
	info, err := churngit.Detect(ctx, opts.Dir)
	if err != nil {
		return Result{}, err
	}

	res := response{
		Query:  opts.Query,
		Budget: opts.Budget,
		Stale:  info.Stale,
		Note:   "recall engine is wired; retrieval over .churn store starts in phase 7",
	}

	if opts.JSON {
		data, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			return Result{}, err
		}
		return Result{Output: string(data)}, nil
	}

	return Result{Output: fmt.Sprintf("query: %q\nbudget: %d\nstale: %t\n%s", res.Query, res.Budget, res.Stale, res.Note)}, nil
}
