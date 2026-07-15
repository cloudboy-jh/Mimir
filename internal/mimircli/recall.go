package mimircli

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

type recallOptions struct {
	Dir    string
	Query  string
	Budget int
	JSON   bool
}

type recallResult struct{ Output string }

type Match struct {
	Kind         string   `json:"kind"`
	Name         string   `json:"name,omitempty"`
	Type         string   `json:"type,omitempty"`
	File         string   `json:"file"`
	Line         int      `json:"line,omitempty"`
	Signature    string   `json:"signature,omitempty"`
	Dependencies []string `json:"dependencies,omitempty"`
	Score        int      `json:"score"`
}

type Response struct {
	Query   string  `json:"query"`
	Budget  int     `json:"budget"`
	Stale   bool    `json:"stale"`
	Matches []Match `json:"matches"`
}

func runRecall(ctx context.Context, opts recallOptions) (recallResult, error) {
	res, err := queryRecall(ctx, opts.Dir, opts.Query, opts.Budget)
	if err != nil {
		return recallResult{}, err
	}
	if opts.JSON {
		data, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			return recallResult{}, err
		}
		return recallResult{Output: string(data)}, nil
	}
	return recallResult{Output: format(res)}, nil
}

func queryRecall(ctx context.Context, dir, query string, budget int) (Response, error) {
	if budget <= 0 {
		budget = 4000
	}
	info, err := detectRepo(ctx, dir)
	if err != nil {
		return Response{}, err
	}
	idx, err := loadIndex(info.Root)
	if err != nil {
		return Response{}, fmt.Errorf("missing .mimir/index.json; run mimir index --full")
	}
	matches := rank(idx, query)
	return Response{Query: query, Budget: budget, Stale: info.Stale, Matches: fit(matches, budget)}, nil
}

func fileDeps(ctx context.Context, dir, file string) (fileInfo, []string, error) {
	info, err := detectRepo(ctx, dir)
	if err != nil {
		return fileInfo{}, nil, err
	}
	idx, err := loadIndex(info.Root)
	if err != nil {
		return fileInfo{}, nil, err
	}
	rel := filepath.ToSlash(file)
	if filepath.IsAbs(file) {
		if r, err := filepath.Rel(info.Root, file); err == nil {
			rel = filepath.ToSlash(r)
		}
	}
	fi := idx.Files[rel]
	downstream := []string{}
	for path, candidate := range idx.Files {
		for _, dep := range candidate.Dependencies {
			if dep == rel {
				downstream = append(downstream, path)
			}
		}
	}
	sort.Strings(downstream)
	return fi, downstream, nil
}

func locateSymbol(ctx context.Context, dir, name string) (symbol, bool, error) {
	info, err := detectRepo(ctx, dir)
	if err != nil {
		return symbol{}, false, err
	}
	idx, err := loadIndex(info.Root)
	if err != nil {
		return symbol{}, false, err
	}
	sym, ok := idx.Symbols[name]
	if ok {
		sym.File = filepath.Join(info.Root, filepath.FromSlash(sym.File))
	}
	return sym, ok, nil
}

func rank(idx mimirIndex, query string) []Match {
	q := strings.ToLower(strings.TrimSpace(query))
	var matches []Match
	for name, sym := range idx.Symbols {
		score := score(q, name, sym.File, sym.Signature, sym.Type)
		if score == 0 {
			continue
		}
		fi := idx.Files[sym.File]
		matches = append(matches, Match{Kind: "symbol", Name: name, Type: sym.Type, File: sym.File, Line: sym.Line, Signature: sym.Signature, Dependencies: fi.Dependencies, Score: score})
	}
	for path, fi := range idx.Files {
		score := score(q, path, strings.Join(fi.Symbols, " "), strings.Join(fi.Dependencies, " "))
		if score == 0 {
			continue
		}
		matches = append(matches, Match{Kind: "file", File: path, Dependencies: fi.Dependencies, Score: score})
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Score == matches[j].Score {
			return matches[i].File < matches[j].File
		}
		return matches[i].Score > matches[j].Score
	})
	return matches
}

func score(q string, parts ...string) int {
	if q == "" {
		return 1
	}
	s := strings.ToLower(strings.Join(parts, " "))
	if strings.Contains(s, q) {
		return 100 + len(q)
	}
	total := 0
	for _, token := range strings.Fields(q) {
		if strings.Contains(s, token) {
			total += 20 + len(token)
		}
	}
	return total
}

func fit(matches []Match, budget int) []Match {
	maxChars := budget * 4
	used := 0
	out := []Match{}
	for _, m := range matches {
		cost := len(m.Kind) + len(m.Name) + len(m.File) + len(m.Signature) + len(strings.Join(m.Dependencies, "")) + 40
		if used+cost > maxChars && len(out) > 0 {
			break
		}
		out = append(out, m)
		used += cost
	}
	return out
}

func format(res Response) string {
	var b strings.Builder
	fmt.Fprintf(&b, "query: %q\nbudget: %d\nstale: %t\n", res.Query, res.Budget, res.Stale)
	if len(res.Matches) == 0 {
		b.WriteString("matches: none\n")
		return b.String()
	}
	for _, m := range res.Matches {
		if m.Kind == "symbol" {
			fmt.Fprintf(&b, "\n[%d] %s %s %s:%d\n", m.Score, m.Type, m.Name, m.File, m.Line)
			if m.Signature != "" {
				fmt.Fprintf(&b, "  %s\n", m.Signature)
			}
		} else {
			fmt.Fprintf(&b, "\n[%d] file %s\n", m.Score, m.File)
		}
		if len(m.Dependencies) > 0 {
			fmt.Fprintf(&b, "  deps: %s\n", strings.Join(m.Dependencies, ", "))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
