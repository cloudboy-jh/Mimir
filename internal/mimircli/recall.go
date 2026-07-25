package mimircli

import (
	"context"
	"encoding/json"

	"github.com/cloudboy-jh/mimir/internal/codeindex"
)

type recallOptions struct {
	Dir    string
	Query  string
	Budget int
	JSON   bool
}

type recallResult struct{ Output string }

func runRecall(ctx context.Context, opts recallOptions) (recallResult, error) {
	result, err := codeindex.Recall(ctx, codeindex.RecallOptions{Dir: opts.Dir, Query: opts.Query, Budget: opts.Budget})
	if err != nil {
		return recallResult{}, err
	}
	if opts.JSON {
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return recallResult{}, err
		}
		return recallResult{Output: string(data)}, nil
	}
	return recallResult{Output: codeindex.Format(result)}, nil
}
