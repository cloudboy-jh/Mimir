package mimircli

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
	Repo    string
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
	repo := filepath.Base(info.Root)
	idx.Repo = repo
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
		Repo:    repo,
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

var errNotRepo = errors.New("not inside a git repository")

type repoInfo struct {
	Root        string
	Branch      string
	HeadSHA     string
	Remote      string
	StorePath   string
	StoreExists bool
	IndexedSHA  string
	Stale       bool
}

type indexManifest struct {
	IndexedCommit string `json:"indexed_commit"`
}

func detectRepo(ctx context.Context, dir string) (repoInfo, error) {
	root, err := runGit(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return repoInfo{}, errNotRepo
	}

	branch, _ := runGit(ctx, root, "branch", "--show-current")
	head, _ := runGit(ctx, root, "rev-parse", "HEAD")
	remote, _ := runGit(ctx, root, "config", "--get", "remote.origin.url")

	storePath := filepath.Join(root, ".mimir")
	manifestPath := filepath.Join(storePath, "index.json")
	info := repoInfo{
		Root:        filepath.Clean(root),
		Branch:      branch,
		HeadSHA:     head,
		Remote:      remote,
		StorePath:   storePath,
		StoreExists: pathExists(manifestPath),
		Stale:       true,
	}

	if info.StoreExists {
		indexedSHA, err := readIndexedSHA(manifestPath)
		if err == nil {
			info.IndexedSHA = indexedSHA
			info.Stale = indexedSHA != head
		}
	}

	return info, nil
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			return "", err
		}
		return "", fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), text, err)
	}
	return text, nil
}

func readIndexedSHA(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var manifest indexManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return "", err
	}
	return manifest.IndexedCommit, nil
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

const mimirDirName = ".mimir"
const mimirIndexFile = "index.json"
const mimirConfigFile = "config.json"

type mimirIndex struct {
	Repo          string              `json:"repo"`
	IndexedCommit string              `json:"indexed_commit"`
	Timestamp     string              `json:"timestamp"`
	Files         map[string]fileInfo `json:"files"`
	Symbols       map[string]symbol   `json:"symbols"`
}

type fileInfo struct {
	Hash         string   `json:"hash"`
	Symbols      []string `json:"symbols"`
	Dependencies []string `json:"dependencies"`
}

type symbol struct {
	Type      string `json:"type"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	Signature string `json:"signature,omitempty"`
}

type mimirConfig struct {
	IgnorePaths []string `json:"ignore_paths"`
	Budget      int      `json:"budget"`
}

func indexPath(root string) string  { return filepath.Join(root, mimirDirName, mimirIndexFile) }
func configPath(root string) string { return filepath.Join(root, mimirDirName, mimirConfigFile) }

func loadIndex(root string) (mimirIndex, error) {
	data, err := os.ReadFile(indexPath(root))
	if err != nil {
		return mimirIndex{}, err
	}
	var idx mimirIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return mimirIndex{}, err
	}
	if idx.Files == nil {
		idx.Files = map[string]fileInfo{}
	}
	if idx.Symbols == nil {
		idx.Symbols = map[string]symbol{}
	}
	return idx, nil
}

func saveIndexAtomic(root string, idx mimirIndex) error {
	dir := filepath.Join(root, mimirDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".index-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write([]byte("\n")); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, indexPath(root))
}

func loadConfig(root string) mimirConfig {
	cfg := mimirConfig{Budget: 4000, IgnorePaths: []string{".git/", ".mimir/", "node_modules/", "vendor/", "dist/", "build/", "coverage/"}}
	data, err := os.ReadFile(configPath(root))
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(data, &cfg)
	if cfg.Budget <= 0 {
		cfg.Budget = 4000
	}
	return cfg
}

func missing(err error) bool { return errors.Is(err, os.ErrNotExist) }

func ignored(path string, cfg mimirConfig) bool {
	p := filepath.ToSlash(path)
	for _, raw := range cfg.IgnorePaths {
		pat := filepath.ToSlash(strings.TrimSpace(raw))
		if pat == "" {
			continue
		}
		if strings.HasSuffix(pat, "/") && strings.HasPrefix(p, pat) {
			return true
		}
		if p == pat || strings.HasPrefix(p, pat+"/") {
			return true
		}
	}
	return false
}
