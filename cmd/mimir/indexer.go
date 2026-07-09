package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type indexOptions struct {
	Dir  string
	Full bool
}

type indexResult struct {
	Message string
	Indexed int
	Removed int
	Mode    string
	Project string
	HeadSHA string
}

var symbolPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^\s*(?:export\s+)?(?:abstract\s+)?class\s+([A-Za-z_$][\w$]*)`),
	regexp.MustCompile(`^\s*(?:export\s+)?interface\s+([A-Za-z_$][\w$]*)`),
	regexp.MustCompile(`^\s*(?:export\s+)?type\s+([A-Za-z_$][\w$]*)\s*=`),
	regexp.MustCompile(`^\s*(?:export\s+)?(?:async\s+)?function\s+([A-Za-z_$][\w$]*)`),
	regexp.MustCompile(`^\s*(?:export\s+)?const\s+([A-Za-z_$][\w$]*)\s*=`),
	regexp.MustCompile(`^\s*func\s+(?:\([^)]*\)\s*)?([A-Za-z_][\w]*)\s*\(`),
	regexp.MustCompile(`^\s*type\s+([A-Za-z_][\w]*)\s+(?:struct|interface|func|map|\[)`),
	regexp.MustCompile(`^\s*class\s+([A-Za-z_][\w]*)`),
	regexp.MustCompile(`^\s*def\s+([A-Za-z_][\w]*)\s*\(`),
	regexp.MustCompile(`^\s*(?:pub\s+)?(?:struct|enum|trait|fn)\s+([A-Za-z_][\w]*)`),
}

func runIndex(ctx context.Context, opts indexOptions) (indexResult, error) {
	info, err := detectRepo(ctx, opts.Dir)
	if err != nil {
		return indexResult{}, err
	}
	cfg := loadConfig(info.Root)
	idx, changed, err := baseIndex(ctx, info.Root, info.HeadSHA, opts.Full)
	if err != nil {
		return indexResult{}, err
	}
	project := filepath.Base(info.Root)
	idx.Project = project
	idx.IndexedCommit = info.HeadSHA
	idx.Timestamp = time.Now().UTC().Format(time.RFC3339)
	if idx.Files == nil {
		idx.Files = map[string]fileInfo{}
	}
	if idx.Symbols == nil {
		idx.Symbols = map[string]symbol{}
	}

	removed := 0
	indexed := 0
	for _, rel := range changed {
		if ignored(rel, cfg) || !supported(rel) {
			continue
		}
		abs := filepath.Join(info.Root, filepath.FromSlash(rel))
		st, err := os.Stat(abs)
		if err != nil || st.IsDir() {
			deleteFile(idx, rel)
			removed++
			continue
		}
		parsed, err := parseFile(info.Root, rel)
		if err != nil {
			continue
		}
		deleteFile(idx, rel)
		idx.Files[rel] = parsed.info
		for name, sym := range parsed.symbols {
			idx.Symbols[name] = sym
		}
		indexed++
	}

	if err := saveIndexAtomic(info.Root, idx); err != nil {
		return indexResult{}, err
	}
	mode := "incremental"
	if opts.Full || !info.StoreExists {
		mode = "full"
	}
	return indexResult{
		Message: fmt.Sprintf("indexed %s: %d updated, %d removed, %d files, %d symbols", mode, indexed, removed, len(idx.Files), len(idx.Symbols)),
		Indexed: indexed,
		Removed: removed,
		Mode:    mode,
		Project: project,
		HeadSHA: info.HeadSHA,
	}, nil
}

func baseIndex(ctx context.Context, root, head string, full bool) (mimirIndex, []string, error) {
	if full {
		paths, err := listFiles(ctx, root)
		return mimirIndex{IndexedCommit: head, Files: map[string]fileInfo{}, Symbols: map[string]symbol{}}, paths, err
	}
	idx, err := loadIndex(root)
	if err != nil {
		paths, listErr := listFiles(ctx, root)
		return mimirIndex{IndexedCommit: head, Files: map[string]fileInfo{}, Symbols: map[string]symbol{}}, paths, listErr
	}
	changed, err := changedFiles(ctx, root, idx.IndexedCommit)
	if err != nil || idx.IndexedCommit == "" {
		paths, listErr := listFiles(ctx, root)
		return idx, paths, listErr
	}
	return idx, changed, nil
}

func listFiles(ctx context.Context, root string) ([]string, error) {
	out, err := runGit(ctx, root, "ls-files", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	paths := splitPaths(out)
	existing := paths[:0]
	for _, p := range paths {
		if st, err := os.Stat(filepath.Join(root, filepath.FromSlash(p))); err == nil && !st.IsDir() {
			existing = append(existing, p)
		}
	}
	return existing, nil
}

func changedFiles(ctx context.Context, root, indexed string) ([]string, error) {
	seen := map[string]bool{}
	if indexed != "" {
		if out, err := runGit(ctx, root, "diff", "--name-only", indexed+"..HEAD"); err == nil {
			for _, p := range splitPaths(out) {
				seen[p] = true
			}
		}
	}
	if out, err := runGit(ctx, root, "diff", "--name-only"); err == nil {
		for _, p := range splitPaths(out) {
			seen[p] = true
		}
	}
	if out, err := runGit(ctx, root, "ls-files", "--others", "--exclude-standard"); err == nil {
		for _, p := range splitPaths(out) {
			seen[p] = true
		}
	}
	paths := make([]string, 0, len(seen))
	for p := range seen {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths, nil
}

func splitPaths(out string) []string {
	var paths []string
	for _, p := range strings.Split(out, "\n") {
		p = filepath.ToSlash(strings.TrimSpace(p))
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths
}

type parsedFile struct {
	info    fileInfo
	symbols map[string]symbol
}

func parseFile(root, rel string) (parsedFile, error) {
	abs := filepath.Join(root, filepath.FromSlash(rel))
	data, err := os.ReadFile(abs)
	if err != nil {
		return parsedFile{}, err
	}
	h := sha256.Sum256(data)
	pf := parsedFile{info: fileInfo{Hash: hex.EncodeToString(h[:])}, symbols: map[string]symbol{}}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	line := 0
	deps := map[string]bool{}
	for scanner.Scan() {
		line++
		text := scanner.Text()
		for _, dep := range importsFromLine(root, rel, text) {
			deps[dep] = true
		}
		for _, re := range symbolPatterns {
			m := re.FindStringSubmatch(text)
			if len(m) < 2 {
				continue
			}
			name := m[1]
			typ := symbolType(strings.TrimSpace(text))
			pf.info.Symbols = append(pf.info.Symbols, name)
			pf.symbols[name] = symbol{Type: typ, File: rel, Line: line, Signature: strings.TrimSpace(text)}
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return parsedFile{}, err
	}
	for dep := range deps {
		pf.info.Dependencies = append(pf.info.Dependencies, dep)
	}
	sort.Strings(pf.info.Symbols)
	sort.Strings(pf.info.Dependencies)
	return pf, nil
}

func deleteFile(idx mimirIndex, rel string) {
	delete(idx.Files, rel)
	for name, sym := range idx.Symbols {
		if sym.File == rel {
			delete(idx.Symbols, name)
		}
	}
}

func supported(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".py", ".rs", ".java", ".cs", ".cpp", ".c", ".h", ".hpp":
		return true
	default:
		return false
	}
}

var importRegexes = []*regexp.Regexp{
	regexp.MustCompile(`from\s+["']([^"']+)["']`),
	regexp.MustCompile(`import\s+["']([^"']+)["']`),
	regexp.MustCompile(`require\(["']([^"']+)["']\)`),
	regexp.MustCompile(`^\s*"([^"]+)"`),
	regexp.MustCompile(`^\s*import\s+([A-Za-z0-9_./-]+)`),
}

func importsFromLine(root, rel, line string) []string {
	var deps []string
	for _, re := range importRegexes {
		for _, m := range re.FindAllStringSubmatch(line, -1) {
			if len(m) > 1 {
				deps = append(deps, resolveImport(root, rel, m[1]))
			}
		}
	}
	return deps
}

func resolveImport(root, rel, dep string) string {
	if !strings.HasPrefix(dep, ".") {
		return dep
	}
	base := filepath.Join(filepath.Dir(filepath.FromSlash(rel)), filepath.FromSlash(dep))
	candidates := []string{base, base + ".ts", base + ".tsx", base + ".js", base + ".jsx", base + ".go", filepath.Join(base, "index.ts"), filepath.Join(base, "index.tsx"), filepath.Join(base, "index.js")}
	for _, c := range candidates {
		if st, err := os.Stat(filepath.Join(root, c)); err == nil && !st.IsDir() {
			return filepath.ToSlash(filepath.Clean(c))
		}
	}
	return filepath.ToSlash(filepath.Clean(base))
}

func symbolType(line string) string {
	for _, t := range []string{"interface", "class", "struct", "enum", "trait", "type", "func", "function", "def", "const", "fn"} {
		if strings.Contains(line, t+" ") || strings.HasPrefix(line, t+" ") || strings.Contains(line, " "+t+" ") {
			return t
		}
	}
	return "symbol"
}
