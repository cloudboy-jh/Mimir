package mimircli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
)

type IO struct {
	In       io.Reader
	Out, Err io.Writer
}

func Execute(ctx context.Context, args []string) error {
	return ExecuteIO(ctx, args, IO{In: os.Stdin, Out: os.Stdout, Err: os.Stderr})
}

func ExecuteIO(ctx context.Context, args []string, ioctx IO) error {
	if ioctx.Out == nil {
		ioctx.Out = os.Stdout
	}
	if ioctx.Err == nil {
		ioctx.Err = os.Stderr
	}
	if len(args) == 0 {
		return usage(ioctx.Out)
	}
	switch args[0] {
	case "--version":
		if len(args) != 1 {
			return fmt.Errorf("usage: mimir --version")
		}
		_, err := fmt.Fprintln(ioctx.Out, versionString())
		return err
	case "version":
		return cmdVersion(args[1:], ioctx.Out)
	case "-h", "--help":
		return usage(ioctx.Out)
	case "help":
		if len(args) == 2 && args[1] == "advanced" {
			return advancedUsage(ioctx.Out)
		}
		return usage(ioctx.Out)
	case "index":
		fs := flag.NewFlagSet("index", flag.ContinueOnError)
		fs.SetOutput(ioctx.Err)
		full := fs.Bool("full", false, "force a full repository index")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		res, err := runIndex(ctx, indexOptions{Dir: ".", Full: *full})
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(ioctx.Out, res.Message)
		return err
	case "recall":
		queryArgs, budget, jsonOut, err := parseRecallArgs(args[1:])
		if err != nil {
			return err
		}
		if len(queryArgs) == 0 {
			return fmt.Errorf("usage: mimir recall <query> [--budget 4000] [--json]")
		}
		res, err := runRecall(ctx, recallOptions{Dir: ".", Query: strings.Join(queryArgs, " "), Budget: budget, JSON: jsonOut})
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(ioctx.Out, res.Output)
		return err
	case "serve":
		return serveMCP(ctx, mcpOptions{Dir: ".", In: ioctx.In, Out: ioctx.Out})
	case "whoami":
		return remotePrint(ctx, ioctx.Out, "GET", "/whoami", nil)
	case "list":
		return cmdList(ctx, args[1:], ioctx.Out)
	case "sessions":
		return remotePrint(ctx, ioctx.Out, "GET", "/sessions", nil)
	case "session":
		return cmdSession(ctx, args[1:], ioctx.Out)
	case "search":
		if len(args) < 2 {
			return fmt.Errorf("usage: mimir search <query>")
		}
		data, err := federatedSearch(ctx, strings.Join(args[1:], " "))
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(ioctx.Out, string(data))
		return err
	case "mark":
		if len(args) != 3 {
			return fmt.Errorf("usage: mimir mark <session> <landed|discarded|abandoned|unresolved|promoted|unknown>")
		}
		return remotePrint(ctx, ioctx.Out, "POST", "/sessions/"+args[1]+"/mark", map[string]string{"outcome": args[2]})
	case "reconcile":
		if len(args) != 1 {
			return fmt.Errorf("usage: mimir reconcile")
		}
		data, err := runReconcile(ctx)
		if err != nil {
			return err
		}
		return printRemoteData(ioctx.Out, data)
	case "config":
		return cmdConfig(ctx, args[1:], ioctx.Out)
	case "setup":
		return setup(ctx, args[1:], ioctx)
	case "deploy":
		return deploy(ctx, args[1:], ioctx)
	case "access":
		return cmdAccess(ctx, args[1:], ioctx)
	case "login":
		return login(ctx, args[1:], ioctx)
	case "install":
		return cmdInstall(args[1:], ioctx.Out)
	case "uninstall":
		return cmdUninstall(args[1:], ioctx.Out)
	case "dashboard":
		return dashboard(ctx, ioctx)
	case "connection":
		return writeConnectionManifest(ioctx.Out)
	case "update":
		return cmdUpdate(ctx, args[1:], ioctx.Out)
	case "doctor":
		return doctor(ctx, args[1:], ioctx.Out)
	case "_post-update", "_install-integrations":
		if len(args) != 1 {
			return fmt.Errorf("usage: mimir _post-update")
		}
		report := refreshLifecycleIntegrations(ctx, "update")
		return json.NewEncoder(ioctx.Out).Encode(report)
	case "outcome":
		if len(args) != 3 || args[1] != "git" {
			return fmt.Errorf("usage: mimir outcome git <session>")
		}
		data, err := markGitOutcome(ctx, args[2])
		if err != nil {
			return err
		}
		return printRemoteData(ioctx.Out, data)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

type versionReport struct {
	Version        string                        `json:"version"`
	Commit         string                        `json:"commit"`
	Date           string                        `json:"date"`
	BundleVersion  string                        `json:"bundle_version,omitempty"`
	ReceiptPath    string                        `json:"receipt_path"`
	ArtifactCounts map[managedArtifactStatus]int `json:"artifact_counts"`
}

type installReport struct {
	Binary    installBinaryReport   `json:"binary"`
	Artifacts managedArtifactReport `json:"artifacts"`
}

type installBinaryReport struct {
	Path      string `json:"path"`
	Status    string `json:"status"`
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
	Hash      string `json:"sha256"`
	Source    string `json:"source"`
	Method    string `json:"method"`
}

func cmdVersion(args []string, out io.Writer) error {
	jsonOutput := false
	if len(args) == 1 && args[0] == "--json" {
		jsonOutput = true
	} else if len(args) != 0 {
		return fmt.Errorf("usage: mimir version [--json]")
	}
	artifacts, err := checkManagedArtifacts()
	if err != nil {
		return err
	}
	receipt, err := loadInstallReceipt()
	if err != nil {
		return err
	}
	report := versionReport{
		Version: version, Commit: commit, Date: date,
		BundleVersion: receipt.BundleVersion, ReceiptPath: artifacts.ReceiptPath,
		ArtifactCounts: managedArtifactCounts(artifacts),
	}
	if jsonOutput {
		return json.NewEncoder(out).Encode(report)
	}
	if _, err := fmt.Fprintln(out, versionString()); err != nil {
		return err
	}
	if receipt.BundleVersion != "" {
		_, err = fmt.Fprintf(out, "Bundle %s · %s\n", receipt.BundleVersion, artifactSummary(artifacts))
	}
	return err
}

func cmdInstall(args []string, out io.Writer) error {
	jsonOutput, binDir := false, ""
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--json":
			jsonOutput = true
		case args[i] == "--bin-dir" && i+1 < len(args):
			if strings.HasPrefix(args[i+1], "-") {
				return fmt.Errorf("--bin-dir requires a value")
			}
			binDir = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--bin-dir="):
			binDir = strings.TrimPrefix(args[i], "--bin-dir=")
		default:
			return fmt.Errorf("usage: mimir install [--bin-dir <dir>] [--json]")
		}
	}
	if strings.TrimSpace(binDir) == "" && containsBinDirArg(args) {
		return fmt.Errorf("--bin-dir requires a value")
	}
	binary, err := bootstrapCurrentExecutable(binDir)
	if err != nil {
		return err
	}
	artifacts, err := syncInstallArtifacts(installReceiptUpdate{
		Source: binary.Source,
		Method: binary.Method,
		CLI: installReceiptCLI{
			Path: binary.Path, Version: binary.Version, Commit: binary.Commit,
			BuildDate: binary.BuildDate, Hash: binary.Hash,
		},
	})
	if err != nil {
		return err
	}
	report := installReport{Binary: binary, Artifacts: artifacts}
	if jsonOutput {
		return json.NewEncoder(out).Encode(report)
	}
	if _, err := fmt.Fprintf(out, "mimir %s  %s  %s\n", binary.Version, binary.Status, binary.Path); err != nil {
		return err
	}
	for _, artifact := range artifacts.Artifacts {
		if _, err := fmt.Fprintf(out, "%s  %s\n", artifact.Status, artifact.Path); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(out, "%s\nInstall log: %s\n", artifacts.Summary, artifacts.LogPath)
	return err
}

func containsBinDirArg(args []string) bool {
	for _, arg := range args {
		if arg == "--bin-dir" || strings.HasPrefix(arg, "--bin-dir=") {
			return true
		}
	}
	return false
}

func cmdUninstall(args []string, out io.Writer) error {
	keepBinary, jsonOutput := false, false
	for _, arg := range args {
		switch arg {
		case "--keep-binary":
			keepBinary = true
		case "--json":
			jsonOutput = true
		default:
			return fmt.Errorf("usage: mimir uninstall [--keep-binary] [--json]")
		}
	}
	report, err := uninstallManagedInstallation(keepBinary)
	if err != nil {
		return err
	}
	if jsonOutput {
		return json.NewEncoder(out).Encode(report)
	}
	if _, err := fmt.Fprintf(out, "binary  %s  %s\n", report.Binary.Status, report.Binary.Path); err != nil {
		return err
	}
	for _, artifact := range report.Artifacts {
		if artifact.Status == artifactUnowned {
			continue
		}
		if _, err := fmt.Fprintf(out, "%s  %s\n", artifact.Status, artifact.Path); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(out, "hermes  %s  %s\n", report.Hermes.State, report.Hermes.Detail); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "%s\nConnection, local Worker files, Cloudflare deployment, and install log preserved.\n", report.Summary)
	return err
}

func bootstrapCurrentExecutable(explicitDir string) (installBinaryReport, error) {
	sourcePath, err := executablePath()
	if err != nil {
		return installBinaryReport{}, fmt.Errorf("locating current executable: %w", err)
	}
	sourcePath, err = filepath.Abs(sourcePath)
	if err != nil {
		return installBinaryReport{}, err
	}
	sourceData, err := os.ReadFile(sourcePath)
	if err != nil {
		return installBinaryReport{}, fmt.Errorf("reading current executable: %w", err)
	}
	temporary := temporaryExecutable(sourcePath)
	target := sourcePath
	method, status := "existing", "current"
	if explicitDir != "" || temporary {
		dir, err := resolveInstallDir(explicitDir)
		if err != nil {
			return installBinaryReport{}, err
		}
		name := "mimir"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		target = filepath.Join(dir, name)
		if symlink, err := pathContainsSymlink(filesystemRoot(target), target); err != nil {
			return installBinaryReport{}, err
		} else if symlink {
			return installBinaryReport{}, fmt.Errorf("refusing to overwrite symlinked executable path %s", target)
		}
		if managedByPackageManager(target) {
			return installBinaryReport{}, fmt.Errorf("refusing to overwrite package-manager-owned path %s", target)
		}
		if resolved, err := filepath.EvalSymlinks(target); err == nil && managedByPackageManager(resolved) {
			return installBinaryReport{}, fmt.Errorf("refusing to overwrite package-manager-owned path %s", resolved)
		}
		if !sameFilePath(sourcePath, target) {
			sourceHash := hashBytes(sourceData)
			info, statErr := os.Lstat(target)
			switch {
			case statErr == nil:
				if !info.Mode().IsRegular() {
					return installBinaryReport{}, fmt.Errorf("refusing to overwrite non-regular executable path %s", target)
				}
				current, err := os.ReadFile(target)
				if err != nil {
					return installBinaryReport{}, err
				}
				currentHash := hashBytes(current)
				if currentHash == sourceHash {
					method, status = "existing", "current"
					break
				}
				receipt, err := loadInstallReceipt()
				if err != nil {
					return installBinaryReport{}, err
				}
				if !sameFilePath(receipt.CLI.Path, target) || receipt.CLI.Hash == "" || receipt.CLI.Hash != currentHash {
					return installBinaryReport{}, fmt.Errorf("refusing to overwrite unowned executable %s", target)
				}
				if err := installExecutableCopy(target, sourceData, currentHash); err != nil {
					return installBinaryReport{}, fmt.Errorf("installing CLI binary: %w", err)
				}
				method, status = "bootstrap-copy", "updated"
			case os.IsNotExist(statErr):
				if err := installExecutableCopy(target, sourceData, ""); err != nil {
					return installBinaryReport{}, fmt.Errorf("installing CLI binary: %w", err)
				}
				method, status = "bootstrap-copy", "installed"
			default:
				return installBinaryReport{}, statErr
			}
		}
	}
	source := "executable"
	if temporary {
		source = "go-run"
	}
	targetHash := hashBytes(sourceData)
	if status == "current" {
		receipt, err := loadInstallReceipt()
		if err != nil {
			return installBinaryReport{}, err
		}
		if sameFilePath(receipt.CLI.Path, target) && receipt.CLI.Hash == targetHash {
			if receipt.Source != "" {
				source = receipt.Source
			}
			if receipt.Method != "" {
				method = receipt.Method
			}
		}
	}
	return installBinaryReport{
		Path: target, Status: status, Version: version, Commit: commit,
		BuildDate: date, Hash: targetHash, Source: source, Method: method,
	}, nil
}

func resolveInstallDir(explicit string) (string, error) {
	dir := strings.TrimSpace(explicit)
	if dir == "" {
		for _, key := range []string{"MIMIR_INSTALL_DIR", "GOBIN"} {
			if value := strings.TrimSpace(os.Getenv(key)); value != "" {
				dir = value
				break
			}
		}
	}
	if dir == "" {
		if paths := filepath.SplitList(os.Getenv("GOPATH")); len(paths) > 0 && strings.TrimSpace(paths[0]) != "" {
			dir = filepath.Join(paths[0], "bin")
		}
	}
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolving install directory: %w", err)
		}
		dir = filepath.Join(home, "go", "bin")
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolving install directory: %w", err)
	}
	return dir, nil
}

func temporaryExecutable(path string) bool {
	temp, err := filepath.Abs(os.TempDir())
	if err != nil {
		return false
	}
	path = filepath.Clean(path)
	rel, err := filepath.Rel(temp, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func sameFilePath(left, right string) bool {
	left, _ = filepath.Abs(left)
	right, _ = filepath.Abs(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
	}
	return filepath.Clean(left) == filepath.Clean(right)
}

// installExecutableCopy installs only when target is absent (expectedHash is
// empty) or still contains the exact receipt-owned bytes checked by the caller.
func installExecutableCopy(target string, data []byte, expectedHash string) error {
	dir := filepath.Dir(target)
	if symlink, err := pathContainsSymlink(filesystemRoot(target), target); err != nil {
		return err
	} else if symlink {
		return fmt.Errorf("refusing to install through symlinked path %s", target)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	staged, err := os.CreateTemp(dir, ".mimir-install-*")
	if err != nil {
		return err
	}
	stagedPath := staged.Name()
	defer os.Remove(stagedPath)
	if _, err := staged.Write(data); err != nil {
		_ = staged.Close()
		return err
	}
	if err := staged.Sync(); err != nil {
		_ = staged.Close()
		return err
	}
	if err := staged.Close(); err != nil {
		return err
	}
	if err := os.Chmod(stagedPath, 0o755); err != nil {
		return err
	}
	if symlink, err := pathContainsSymlink(filesystemRoot(target), target); err != nil {
		return err
	} else if symlink {
		return fmt.Errorf("refusing to replace symlinked path %s", target)
	}
	if err := validateExecutableReplacement(target, expectedHash); err != nil {
		return err
	}
	if expectedHash == "" {
		return os.Rename(stagedPath, target)
	}
	if runtime.GOOS == "windows" {
		old := target + ".old"
		_ = os.Remove(old)
		if err := os.Rename(target, old); err != nil {
			return err
		}
		if err := os.Rename(stagedPath, target); err != nil {
			_ = os.Rename(old, target)
			return err
		}
		_ = os.Remove(old)
		return nil
	}
	return os.Rename(stagedPath, target)
}

func validateExecutableReplacement(target, expectedHash string) error {
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		if expectedHash == "" {
			return nil
		}
		return fmt.Errorf("refusing to replace executable %s: owned target disappeared", target)
	}
	if err != nil {
		return err
	}
	if expectedHash == "" {
		return fmt.Errorf("refusing to overwrite unowned executable %s", target)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to overwrite non-regular executable path %s", target)
	}
	current, err := os.ReadFile(target)
	if err != nil {
		return err
	}
	if hashBytes(current) != expectedHash {
		return fmt.Errorf("refusing to replace executable %s: current hash no longer matches install receipt", target)
	}
	return nil
}

func cmdSession(ctx context.Context, args []string, out io.Writer) error {
	if len(args) > 0 && args[0] == "status" && len(args) < 2 {
		return fmt.Errorf("usage: mimir session status <id> [--json]")
	}
	if len(args) > 0 && args[0] == "end" && len(args) < 2 {
		return fmt.Errorf("usage: mimir session end <id> [--outcome landed|discarded|abandoned|unresolved] [--reason text]")
	}
	if len(args) == 1 {
		return remotePrint(ctx, out, "GET", "/sessions/"+args[0], nil)
	}
	if len(args) >= 2 && args[0] == "end" {
		id, body, err := parseSessionEndArgs(args[1:])
		if err != nil {
			return err
		}
		status, err := endRemoteSession(ctx, id, body)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, endedReceiptText(status))
		return err
	}
	if len(args) >= 2 && args[0] == "status" {
		if len(args) > 3 || (len(args) == 3 && args[2] != "--json") {
			return fmt.Errorf("usage: mimir session status <id> [--json]")
		}
		return printSessionStatus(ctx, out, args[1], len(args) == 3)
	}
	if len(args) >= 3 && args[0] == "outcome" {
		id, outcome, reason, err := parseSessionOutcomeArgs(args[1:])
		if err != nil {
			return err
		}
		body := map[string]any{"outcome": outcome, "source": "agent"}
		if reason != "" {
			body["reason"] = reason
		}
		return remotePrint(ctx, out, "POST", "/sessions/"+id+"/outcome", body)
	}
	return fmt.Errorf("usage: mimir session <id> | mimir session status <id> [--json] | mimir session end <id> [--outcome value] [--reason text] | mimir session outcome <id> <landed|discarded|abandoned|unresolved> [--reason text]")
}

func parseSessionEndArgs(args []string) (string, map[string]any, error) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return "", nil, fmt.Errorf("usage: mimir session end <id> [--outcome landed|discarded|abandoned|unresolved] [--reason text]")
	}
	id := args[0]
	body := map[string]any{}
	for i := 1; i < len(args); i++ {
		switch {
		case args[i] == "--outcome" && i+1 < len(args):
			if _, exists := body["outcome"]; exists {
				return "", nil, fmt.Errorf("--outcome may only be specified once")
			}
			body["outcome"] = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--outcome="):
			if _, exists := body["outcome"]; exists {
				return "", nil, fmt.Errorf("--outcome may only be specified once")
			}
			body["outcome"] = strings.TrimPrefix(args[i], "--outcome=")
		case args[i] == "--reason" && i+1 < len(args):
			if _, exists := body["reason"]; exists {
				return "", nil, fmt.Errorf("--reason may only be specified once")
			}
			body["reason"] = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--reason="):
			if _, exists := body["reason"]; exists {
				return "", nil, fmt.Errorf("--reason may only be specified once")
			}
			body["reason"] = strings.TrimPrefix(args[i], "--reason=")
		default:
			return "", nil, fmt.Errorf("unexpected argument %q", args[i])
		}
	}
	if value, exists := body["outcome"]; exists {
		outcome, _ := value.(string)
		if !canonicalOutcome(outcome) {
			return "", nil, fmt.Errorf("invalid outcome %q: must be landed, discarded, abandoned, or unresolved", outcome)
		}
	}
	if _, hasReason := body["reason"]; hasReason {
		if _, hasOutcome := body["outcome"]; !hasOutcome {
			return "", nil, fmt.Errorf("--reason requires --outcome")
		}
	}
	return id, body, nil
}

func printSessionStatus(ctx context.Context, out io.Writer, id string, jsonOutput bool) error {
	status, err := getSessionStatus(ctx, id)
	if err != nil {
		return err
	}
	if jsonOutput {
		data, err := sessionStatusJSON(status)
		if err != nil {
			return err
		}
		return printRemoteData(out, data)
	}
	lastSaved := "never"
	if status.Capture.LastSavedAt != nil {
		lastSaved = *status.Capture.LastSavedAt
	}
	_, err = fmt.Fprintf(out, "%s\nSession   %s\nCapture   %s\nSaved     %d\nPending   %d\nFailed    %d\nLast save %s\nOutcome   %s\n", receiptSummary(status), status.SessionID, displayState(status.Capture.Status), status.Capture.SavedExchanges, status.Capture.PendingExchanges, status.Capture.FailedExchanges, lastSaved, displayState(status.Outcome))
	if err == nil && status.DashboardURL != nil {
		_, err = fmt.Fprintf(out, "Dashboard %s\n", *status.DashboardURL)
	}
	return err
}

func displayState(value string) string {
	if value == "" {
		return "Unavailable"
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func parseSessionOutcomeArgs(args []string) (id, outcome, reason string, err error) {
	if len(args) < 2 || strings.HasPrefix(args[0], "-") || strings.HasPrefix(args[1], "-") {
		return "", "", "", fmt.Errorf("usage: mimir session outcome <id> <landed|discarded|abandoned|unresolved> [--reason text]")
	}
	id, outcome = args[0], args[1]
	if !canonicalOutcome(outcome) {
		return "", "", "", fmt.Errorf("invalid outcome %q: must be landed, discarded, abandoned, or unresolved", outcome)
	}
	for i := 2; i < len(args); i++ {
		switch {
		case args[i] == "--reason":
			if reason != "" || i+1 >= len(args) {
				return "", "", "", fmt.Errorf("--reason requires one value")
			}
			reason = args[i+1]
			if strings.TrimSpace(reason) == "" {
				return "", "", "", fmt.Errorf("--reason requires one value")
			}
			i++
		case strings.HasPrefix(args[i], "--reason="):
			if reason != "" {
				return "", "", "", fmt.Errorf("--reason may only be specified once")
			}
			reason = strings.TrimPrefix(args[i], "--reason=")
			if strings.TrimSpace(reason) == "" {
				return "", "", "", fmt.Errorf("--reason requires one value")
			}
		default:
			return "", "", "", fmt.Errorf("unexpected argument %q", args[i])
		}
	}
	return id, outcome, reason, nil
}

func canonicalOutcome(outcome string) bool {
	switch outcome {
	case "landed", "discarded", "abandoned", "unresolved":
		return true
	default:
		return false
	}
}

func cmdConfig(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 1 && args[0] == "get" {
		return remotePrint(ctx, out, "GET", "/config", nil)
	}
	if len(args) == 3 && args[0] == "set" {
		var value any
		if err := json.Unmarshal([]byte(args[2]), &value); err != nil {
			value = args[2]
		}
		return remotePrint(ctx, out, "PUT", "/config", map[string]any{args[1]: value})
	}
	return fmt.Errorf("usage: mimir config get | mimir config set <key> <json-value>")
}

func remotePrint(ctx context.Context, out io.Writer, method, path string, body any) error {
	data, err := remoteRequest(ctx, method, path, body)
	if err != nil {
		return err
	}
	return printRemoteData(out, data)
}

func printRemoteData(out io.Writer, data []byte) error {
	var formatted any
	if json.Unmarshal(data, &formatted) == nil {
		data, _ = json.MarshalIndent(formatted, "", "  ")
	}
	_, err := fmt.Fprintln(out, string(data))
	return err
}

func usage(out io.Writer) error {
	_, err := fmt.Fprintln(out, `mimir remembers

Usage:
  mimir setup [--quick] [--json]
  mimir install [--bin-dir <dir>] [--json]
  mimir uninstall [--keep-binary] [--json]
  mimir deploy [--json]
  mimir access [--token <api-token> | --aud <tag> --team-domain <domain>]
  mimir login [--json]
  mimir dashboard
  mimir list [--repo name] [--outcome landed|discarded|abandoned|unresolved] [--limit 20]
  mimir session status <id> [--json]
  mimir session end <id> [--outcome landed|discarded|abandoned|unresolved] [--reason text]
  mimir session outcome <id> <landed|discarded|abandoned|unresolved> [--reason text]
  mimir reconcile
  mimir doctor [--json]
  mimir update [--check] [--json]
  mimir version [--json]

Run "mimir help advanced" for diagnostic commands.`)
	return err
}

func advancedUsage(out io.Writer) error {
	_, err := fmt.Fprintln(out, `mimir advanced commands

These commands support harness integrations, diagnostics, and development.

Usage:
  mimir connection
  mimir doctor [--json]
  mimir whoami
  mimir list [--repo name] [--outcome landed|discarded|abandoned|unresolved] [--limit 20]
  mimir sessions
  mimir session <id>
  mimir session status <id>
  mimir session end <id> [--outcome landed|discarded|abandoned|unresolved] [--reason text]
  mimir session outcome <id> <landed|discarded|abandoned|unresolved> [--reason text]
  mimir search <query>
  mimir reconcile
  mimir mark <session> <landed|discarded|abandoned|unresolved|promoted|unknown>
  mimir outcome git <session>
  mimir config get
  mimir config set <key> <json-value>
  mimir index [--full]
  mimir recall <query> [--budget 4000] [--json]
  mimir serve`)
	return err
}

func parseRecallArgs(args []string) ([]string, int, bool, error) {
	budget, jsonOut := 4000, false
	query := []string{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json":
			jsonOut = true
		case "--budget":
			if i+1 >= len(args) {
				return nil, 0, false, fmt.Errorf("--budget requires a value")
			}
			if _, err := fmt.Sscanf(args[i+1], "%d", &budget); err != nil || budget <= 0 {
				return nil, 0, false, fmt.Errorf("invalid --budget value")
			}
			i++
		default:
			query = append(query, args[i])
		}
	}
	return query, budget, jsonOut, nil
}

var (
	version = "0.0.0-dev"
	commit  = "unknown"
	date    = "unknown"
)

func SetBuildInfo(buildVersion, buildCommit, buildDate string) {
	if info, ok := debug.ReadBuildInfo(); ok {
		buildVersion, buildCommit, buildDate = resolveBuildInfo(buildVersion, buildCommit, buildDate, info)
	}
	version = buildVersion
	commit = buildCommit
	date = buildDate
}

func resolveBuildInfo(buildVersion, buildCommit, buildDate string, info *debug.BuildInfo) (string, string, string) {
	if (buildVersion == "" || buildVersion == "0.0.0" || buildVersion == "0.0.0-dev") && info.Main.Version != "" && info.Main.Version != "(devel)" {
		buildVersion = strings.TrimPrefix(info.Main.Version, "v")
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			if (buildCommit == "" || buildCommit == "unknown") && setting.Value != "" {
				buildCommit = setting.Value
				if len(buildCommit) > 12 {
					buildCommit = buildCommit[:12]
				}
			}
		case "vcs.time":
			if buildDate == "" || buildDate == "unknown" {
				buildDate = setting.Value
			}
		}
	}
	return buildVersion, buildCommit, buildDate
}

func versionString() string {
	if commit == "unknown" {
		return version
	}
	return version + " (" + commit + ")"
}
