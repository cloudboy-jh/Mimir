package sessionimport

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// OhMyPiAdapter repairs hierarchy only. OMP exchanges already use canonical
// live-capture IDs; replaying them through the Pi importer would duplicate turns
// and historical lifecycle events would change existing state.
type OhMyPiAdapter struct {
	Roots []string
	MaxFiles int
}

func NewOhMyPiAdapter(roots ...string) OhMyPiAdapter { return OhMyPiAdapter{Roots: roots} }
func (OhMyPiAdapter) Name() string { return "oh-my-pi" }
func (a OhMyPiAdapter) Discover(ctx context.Context) ([]Session, error) {
	return a.DiscoverWithOptions(ctx, Options{})
}

var ompAgentName = regexp.MustCompile(`^[A-Za-z0-9_-]+(?:\.[A-Za-z0-9_-]+)*$`)
var ompProfileName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
var ompReservedProfileName = regexp.MustCompile(`(?i)^(?:CON|PRN|AUX|NUL|COM[0-9]|LPT[0-9])(?:\..*)?$`)

func ompSessionRoots() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil { return nil, err }
	config := os.Getenv("PI_CONFIG_DIR")
	if config == "" { config = ".omp" }
	profile, exists := os.LookupEnv("OMP_PROFILE")
	if !exists { profile = os.Getenv("PI_PROFILE") }
	profile = strings.TrimSpace(profile)
	if profile == "default" { profile = "" }
	if profile != "" && (!ompProfileName.MatchString(profile) || strings.HasSuffix(profile, ".") || ompReservedProfileName.MatchString(profile)) {
		return nil, errors.New("invalid OMP profile; set PI_CODING_AGENT_DIR to the source agent directory in the default profile")
	}
	base := filepath.Join(home, config)
	if profile != "" { base = filepath.Join(base, "profiles", profile) }
	agent := filepath.Join(base, "agent")
	if override := os.Getenv("PI_CODING_AGENT_DIR"); override != "" && profile == "" {
		agent = override
	} else if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		if data := os.Getenv("XDG_DATA_HOME"); data != "" {
			xdg := filepath.Join(data, "omp")
			if profile != "" { xdg = filepath.Join(xdg, "profiles", profile) }
			if info, err := os.Stat(xdg); err == nil && info.IsDir() { agent = xdg }
		}
	}
	return []string{filepath.Join(agent, "sessions")}, nil
}

type ompHeader struct {
	Type string `json:"type"`
	ID string `json:"id"`
	Title string `json:"title"`
	Timestamp string `json:"timestamp"`
	CWD string `json:"cwd"`
	ParentSessionID string `json:"parentSessionId"`
}

// OMP writes a session header first, or immediately after its fixed title slot.
// Never inspect prompts/messages or infer UUIDs from transcript filenames.
func readOMPHeader(path string) (ompHeader, error) {
	file, err := os.Open(path)
	if err != nil { return ompHeader{}, err }
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 64<<10)
	var title string
	for line := 0; line < 2 && scanner.Scan(); line++ {
		var header ompHeader
		if err := json.Unmarshal(scanner.Bytes(), &header); err != nil { return ompHeader{}, err }
		if line == 0 && header.Type == "title" { title = header.Title; continue }
		if header.Type != "session" || strings.TrimSpace(header.ID) == "" {
			return ompHeader{}, errors.New("missing exact JSONL session header ID")
		}
		if title != "" { header.Title = title }
		return header, nil
	}
	if err := scanner.Err(); err != nil { return ompHeader{}, err }
	return ompHeader{}, errors.New("missing exact JSONL session header ID")
}

func (a OhMyPiAdapter) DiscoverWithOptions(ctx context.Context, options Options) ([]Session, error) {
	if !selected(options.Sources, a.Name()) { return nil, nil }
	roots := a.Roots
	if len(roots) == 0 {
		var err error
		roots, err = ompSessionRoots()
		if err != nil { return nil, err }
	}
	maxFiles := a.MaxFiles
	if maxFiles <= 0 { maxFiles = DefaultMaxFiles }
	paths := make([]string, 0)
	seen := make(map[string]bool)
	entries := 0
	for _, root := range roots {
		root, err := filepath.Abs(root)
		if err != nil { return nil, err }
		err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if errors.Is(err, fs.ErrNotExist) && path == root { return fs.SkipDir }
			if err != nil { return err }
			if err := ctx.Err(); err != nil { return err }
			entries++
			if entries > maxFiles*10 { return errors.New("OMP artifact entry count exceeds limit") }
			if entry.Type()&os.ModeSymlink != 0 { return nil }
			if entry.IsDir() {
				rel, _ := filepath.Rel(root, path)
				if strings.Count(rel, string(filepath.Separator)) > 32 { return errors.New("OMP artifact depth exceeds limit") }
				return nil
			}
			if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".jsonl") || strings.Contains(entry.Name(), ".bak") || strings.HasPrefix(entry.Name(), "__advisor") { return nil }
			if seen[path] { return nil }
			if len(paths) >= maxFiles { return errors.New("OMP session file count exceeds limit") }
			seen[path] = true
			paths = append(paths, path)
			return nil
		})
		if err != nil { return nil, err }
	}
	sessions := make([]Session, len(paths))
	headers := make([]ompHeader, len(paths))
	byPath := make(map[string]int, len(paths))
	byID := make(map[string][]int, len(paths))
	for i, path := range paths {
		if err := ctx.Err(); err != nil { return nil, err }
		byPath[path] = i
		header, err := readOMPHeader(path)
		session := Session{Harness: a.Name(), SourceID: path, ParentLinkStatus: "unresolved"}
		if err != nil {
			session.ParentLinkReason = fmt.Sprintf("reading header: %v", err)
		} else {
			headers[i] = header
			session.ID, session.SourceID = canonicalID(a.Name(), header.ID), header.ID
			session.Title, session.Directory, session.Repo = header.Title, header.CWD, repoName(header.CWD)
			session.StartedAt = piTimestamp(header.Timestamp)
			byID[session.ID] = append(byID[session.ID], i)
		}
		sessions[i] = session
	}
	for i, path := range paths {
		session := &sessions[i]
		if session.ID == "" { continue }
		if len(byID[session.ID]) != 1 {
			session.ParentLinkReason = "ambiguous session ID appears in multiple artifact files"
			continue
		}
		ownerPath := filepath.Dir(path) + ".jsonl"
		owner, hasOwner := byPath[ownerPath]
		parent := -1
		if explicit := headers[i].ParentSessionID; explicit != "" {
			matches := byID[canonicalID(a.Name(), explicit)]
			if len(matches) == 1 { parent = matches[0] }
			session.ParentLinkEvidence = "session-header-parentSessionId"
		} else if hasOwner {
			// AgentOutputManager reserves dots for parent prefixes. The executor
			// passes parentTaskPrefix=id; structured-subagent writes inside the
			// spawning session's stem directory. Shared artifact roots may also
			// contain A.B.jsonl beside A.jsonl; resolve that exact prefix, not root.
			name := strings.TrimSuffix(filepath.Base(path), ".jsonl")
			if !ompAgentName.MatchString(name) {
				session.ParentLinkReason = "artifact name does not establish exact agent ancestry"
				continue
			}
			if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
				prefix := name[:dot]
				if strings.TrimSuffix(filepath.Base(ownerPath), ".jsonl") == prefix {
					parent = owner
				} else if sibling, ok := byPath[filepath.Join(filepath.Dir(path), prefix+".jsonl")]; ok {
					parent = sibling
				}
			} else {
				parent = owner
			}
			session.ParentLinkEvidence = "omp-artifact-tree"
		} else {
			session.ParentLinkReason = "no exact spawning parent in bounded local artifact tree (root or missing history)"
			continue
		}
		if parent < 0 || sessions[parent].ID == "" || len(byID[sessions[parent].ID]) != 1 {
			session.ParentLinkReason = "exact parent header missing or ambiguous in bounded local artifact tree"
			continue
		}
		if parent == i {
			session.ParentLinkReason = "self-parent relationship rejected"
			continue
		}
		session.ParentSessionID = sessions[parent].ID
		session.ParentLinkStatus = "pending"
		session.ParentLinkReason = "exact local parent; canonical remote records checked on apply"
	}
	// Header metadata may be malformed or copied. Refuse every edge that enters
	// a cycle rather than relying on mutation order to select an arbitrary tree.
	for i := range sessions {
		visited := make(map[string]bool)
		for id := sessions[i].ID; id != ""; {
			if visited[id] {
				sessions[i].ParentLinkStatus = "unresolved"
				sessions[i].ParentLinkReason = "cyclic local parent metadata"
				break
			}
			visited[id] = true
			matches := byID[id]
			if len(matches) != 1 { break }
			id = sessions[matches[0]].ParentSessionID
		}
	}
	return filterSessions(sessions, options), nil
}
