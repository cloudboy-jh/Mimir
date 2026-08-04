package hooks

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	mimirassets "github.com/cloudboy-jh/mimir"
	"github.com/cloudboy-jh/mimir/internal/mimirapi"
)

const (
	MaxInputBytes  = 1024 * 1024
	maxOutboxItems = 1000
	maxOutboxBytes = 64 * 1024 * 1024
	maxFlushItems  = 25
	flushTimeout   = 2 * time.Second
	stateMaxAge    = 7 * 24 * time.Hour
	outboxMaxAge   = 30 * 24 * time.Hour
)

var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type Delivery struct {
	Kind      string         `json:"kind"`
	Harness   string         `json:"harness"`
	SessionID string         `json:"session_id,omitempty"`
	Repo      string         `json:"repo,omitempty"`
	Body      map[string]any `json:"body"`
}

type promptState struct {
	Prompt string `json:"prompt"`
	Key    string `json:"key"`
	Model  string `json:"model,omitempty"`
	CWD    string `json:"cwd,omitempty"`
	Title  string `json:"title,omitempty"`
	At     string `json:"at"`
}

type protectedFile struct {
	Version    int    `json:"version"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

type normalizedInput struct {
	deliveries []Delivery
	clearState string
}

type Service struct {
	Home    string
	Now     func() time.Time
	Deliver func(context.Context, Delivery) error
	key     []byte
}

func New() (Service, error) {
	home, err := mimirHome()
	if err != nil {
		return Service{}, err
	}
	key, err := loadStorageKey(home)
	if err != nil {
		return Service{}, err
	}
	if err := verifyManagedExecutable(home); err != nil {
		return Service{}, err
	}
	pointer, err := mimirapi.LoadPointer()
	if err != nil {
		return Service{Home: home, key: key}, nil
	}
	client := mimirapi.New(pointer)
	return Service{Home: home, key: key, Deliver: func(ctx context.Context, delivery Delivery) error {
		path := ""
		switch delivery.Kind {
		case "event":
			path = "/sessions/" + delivery.SessionID + "/events"
		case "exchange":
			path = "/sessions/" + delivery.SessionID + "/exchanges"
		case "load":
			path = "/integrations/harness-loads"
		default:
			return fmt.Errorf("unknown hook delivery kind %q", delivery.Kind)
		}
		if delivery.Kind != "exchange" {
			_, err := client.Request(ctx, http.MethodPost, path, delivery.Body)
			return err
		}
		data, err := json.Marshal(delivery.Body)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(pointer.URL, "/")+path, bytes.NewReader(data))
		if err != nil {
			return err
		}
		req.Header.Set("authorization", "Bearer "+pointer.Token)
		req.Header.Set("content-type", "application/json")
		req.Header.Set("x-mimir-harness", delivery.Harness)
		if delivery.Repo != "" {
			req.Header.Set("x-mimir-repo", delivery.Repo)
		}
		res, err := client.HTTPClient.Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64*1024))
		if res.StatusCode < 200 || res.StatusCode > 299 {
			return fmt.Errorf("Mimir API %s", res.Status)
		}
		return nil
	}}, nil
}

func (s Service) Ingest(ctx context.Context, harness string, input io.Reader) error {
	if !knownHarness(harness) {
		return fmt.Errorf("unsupported hook harness %q", harness)
	}
	if s.Now == nil {
		s.Now = time.Now
	}
	if err := s.ensureDirs(); err != nil {
		return err
	}
	if err := s.cleanupStale(); err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(input, MaxInputBytes+1))
	if err != nil {
		return err
	}
	if len(data) > MaxInputBytes {
		return fmt.Errorf("hook input exceeds %d bytes", MaxInputBytes)
	}
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return fmt.Errorf("decoding hook input: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("hook input must contain one JSON object")
	}
	normalized, err := s.normalize(harness, payload)
	if err != nil {
		return err
	}
	for _, delivery := range normalized.deliveries {
		if err := s.queue(delivery); err != nil {
			return err
		}
	}
	if normalized.clearState != "" {
		if err := os.Remove(normalized.clearState); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	_ = s.Flush(ctx)
	return nil
}

func (s Service) Flush(ctx context.Context) error {
	if s.Now == nil {
		s.Now = time.Now
	}
	release, err := acquireFileLock(filepath.Join(s.Home, "hook-outbox.flush.lock"), 50*time.Millisecond)
	if err != nil {
		return err
	}
	defer release()
	if err := s.cleanupStale(); err != nil {
		return err
	}
	dir := filepath.Join(s.Home, "hook-outbox")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	flushCtx, cancel := context.WithTimeout(ctx, flushTimeout)
	defer cancel()
	flushed := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if flushed >= maxFlushItems {
			break
		}
		if err := flushCtx.Err(); err != nil {
			return err
		}
		path := filepath.Join(dir, entry.Name())
		data, legacy, err := s.readProtected(path, "outbox")
		if err != nil {
			return err
		}
		var delivery Delivery
		if err := json.Unmarshal(data, &delivery); err != nil {
			return errors.New("decoding queued hook delivery")
		}
		if legacy && len(s.key) != 0 {
			if err := s.writeProtected(dir, path, data, "outbox"); err != nil {
				return err
			}
		}
		if s.Deliver == nil {
			return errors.New("Mimir is not connected")
		}
		flushed++
		if err := s.Deliver(flushCtx, delivery); err != nil {
			return err
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (s Service) normalize(harness string, body map[string]any) (normalizedInput, error) {
	event := stringValue(body["hook_event_name"])
	session := stringValue(body["session_id"])
	if harness == "cursor" && session == "" {
		session = stringValue(body["conversation_id"])
	}
	if session == "" {
		return normalizedInput{}, errors.New("hook input has no session identifier")
	}
	session = canonicalID(session)
	now := s.Now().UTC().Format(time.RFC3339Nano)
	cwd := stringValue(body["cwd"])
	if cwd == "" {
		if roots, ok := body["workspace_roots"].([]any); ok && len(roots) > 0 {
			cwd = stringValue(roots[0])
		}
	}
	repo := filepath.Base(filepath.Clean(cwd))
	if repo == "." || repo == string(filepath.Separator) {
		repo = ""
	}
	key := firstString(body, "prompt_id", "turn_id", "generation_id")
	if key == "" {
		key = now
	}
	model := stringValue(body["model"])
	title := firstString(body, "title", "session_title")
	start, prompt, complete, end := eventNames(harness)
	switch event {
	case start:
		state, err := s.readState(session)
		if err != nil && !os.IsNotExist(err) {
			return normalizedInput{}, err
		}
		if state.Prompt == "" {
			if err := s.writeState(session, promptState{Key: key, Model: model, CWD: cwd, Title: title, At: now}); err != nil {
				return normalizedInput{}, err
			}
		}
		return normalizedInput{deliveries: []Delivery{s.loadDelivery(harness), eventDelivery(harness, session, repo, "heartbeat", now, "", title)}}, nil
	case prompt:
		text := boundedString(stringValue(body["prompt"]), 512*1024)
		if text == "" {
			return normalizedInput{}, errors.New("prompt hook has no prompt")
		}
		if prior, err := s.readState(session); err == nil {
			if model == "" {
				model = prior.Model
			}
			if cwd == "" {
				cwd = prior.CWD
			}
			if title == "" {
				title = prior.Title
			}
		} else if !os.IsNotExist(err) {
			return normalizedInput{}, err
		}
		return normalizedInput{}, s.writeState(session, promptState{Prompt: text, Key: key, Model: model, CWD: cwd, Title: title, At: now})
	case complete:
		response := stringValue(body["last_assistant_message"])
		if harness == "cursor" {
			response = stringValue(body["text"])
		}
		response = boundedString(response, 512*1024)
		state, err := s.readState(session)
		if err != nil || state.Prompt == "" || response == "" {
			return normalizedInput{deliveries: []Delivery{eventDelivery(harness, session, repo, "heartbeat", now, "", title)}}, nil
		}
		if model == "" {
			model = state.Model
		}
		if model == "" {
			model = "unknown"
		}
		if repo == "" && state.CWD != "" {
			repo = filepath.Base(filepath.Clean(state.CWD))
		}
		if title == "" {
			title = state.Title
		}
		exchangeKey := key
		if exchangeKey == now {
			exchangeKey = state.Key
		}
		delivery := exchangeDelivery(harness, session, repo, exchangeKey, now, model, state.Prompt, response, title)
		return normalizedInput{deliveries: []Delivery{delivery}, clearState: s.statePath(session)}, nil
	case "PreCompact", "PostCompact", "preCompact", "StopFailure", "stop":
		return normalizedInput{deliveries: []Delivery{eventDelivery(harness, session, repo, "heartbeat", now, "", title)}}, nil
	case end:
		reason := stringValue(body["reason"])
		if reason == "" {
			reason = "explicit"
		}
		return normalizedInput{deliveries: []Delivery{eventDelivery(harness, session, repo, "end", now, boundedString(reason, 2000), title)}, clearState: s.statePath(session)}, nil
	default:
		return normalizedInput{}, fmt.Errorf("unsupported %s hook event %q", harness, event)
	}
}

func eventNames(harness string) (string, string, string, string) {
	if harness == "cursor" {
		return "sessionStart", "beforeSubmitPrompt", "afterAgentResponse", "sessionEnd"
	}
	return "SessionStart", "UserPromptSubmit", "Stop", "SessionEnd"
}

func eventDelivery(harness, session, repo, kind, ts, reason, title string) Delivery {
	body := map[string]any{"version": 1, "kind": kind, "session_id": session, "harness": harness, "ts": ts}
	if repo != "" {
		body["repo"] = repo
	}
	if reason != "" {
		body["reason"] = reason
	}
	if title != "" {
		body["title"] = boundedString(title, 200)
	}
	return Delivery{Kind: "event", Harness: harness, SessionID: session, Repo: repo, Body: body}
}

func exchangeDelivery(harness, session, repo, key, ts, model, prompt, response, title string) Delivery {
	id := canonicalID(harness + ":" + session + ":" + key)
	provider := map[string]string{"claude-code": "anthropic", "codex": "openai"}[harness]
	body := map[string]any{
		"exchange_id": id, "ts": ts, "model": model, "request_kind": "primary",
		"request":  map[string]any{"messages": []any{map[string]any{"role": "user", "content": prompt}}},
		"response": map[string]any{"role": "assistant", "content": response},
		"usage":    map[string]any{"input_tokens": 0, "output_tokens": 0}, "latency_ms": 0,
	}
	if provider != "" {
		body["provider"] = provider
	}
	if title != "" {
		body["title"] = boundedString(title, 200)
	}
	return Delivery{Kind: "exchange", Harness: harness, SessionID: session, Repo: repo, Body: body}
}

func (s Service) loadDelivery(harness string) Delivery {
	source := map[string]string{"claude-code": "plugins/claude-code/hooks/hooks.json", "codex": "plugins/codex/hooks.json", "cursor": "plugins/cursor/hooks.json"}[harness]
	data, err := os.ReadFile(installedHookPath(harness))
	if err != nil {
		data, _ = mimirassets.Bundle.ReadFile(source)
	}
	sum := sha256.Sum256(data)
	body := map[string]any{"version": 1, "harness": harness, "source_sha256": hex.EncodeToString(sum[:])}
	var receipt struct {
		BundleVersion  string                           `json:"bundle_version"`
		InstallationID string                           `json:"installation_id"`
		CLI            struct{ Version, Commit string } `json:"cli"`
	}
	if data, err := os.ReadFile(filepath.Join(s.Home, "install-receipt.json")); err == nil && json.Unmarshal(data, &receipt) == nil {
		body["bundle_version"], body["installation_id"] = receipt.BundleVersion, receipt.InstallationID
		body["cli_version"], body["cli_commit"] = receipt.CLI.Version, receipt.CLI.Commit
		for key, value := range body {
			if value == "" {
				delete(body, key)
			}
		}
	}
	return Delivery{Kind: "load", Harness: harness, Body: body}
}

func installedHookPath(harness string) string {
	home, _ := os.UserHomeDir()
	switch harness {
	case "claude-code":
		if configured := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); configured != "" {
			home = configured
		} else {
			home = filepath.Join(home, ".claude")
		}
		return filepath.Join(home, "skills", "mimir", "hooks", "hooks.json")
	case "codex":
		if configured := strings.TrimSpace(os.Getenv("CODEX_HOME")); configured != "" {
			home = configured
		} else {
			home = filepath.Join(home, ".codex")
		}
		return filepath.Join(home, "hooks.json")
	default:
		return filepath.Join(home, ".cursor", "hooks.json")
	}
}

func verifyManagedExecutable(home string) error {
	var receipt struct {
		CLI struct {
			Path string `json:"path"`
			Hash string `json:"sha256"`
		} `json:"cli"`
	}
	data, err := os.ReadFile(filepath.Join(home, "install-receipt.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || json.Unmarshal(data, &receipt) != nil || receipt.CLI.Path == "" || receipt.CLI.Hash == "" {
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	samePath := filepath.Clean(executable) == filepath.Clean(receipt.CLI.Path)
	if runtime.GOOS == "windows" {
		samePath = strings.EqualFold(filepath.Clean(executable), filepath.Clean(receipt.CLI.Path))
	}
	if !samePath {
		return errors.New("refusing hook input because the running Mimir binary is not receipt-owned")
	}
	binary, err := os.ReadFile(executable)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(binary)
	if hex.EncodeToString(sum[:]) != receipt.CLI.Hash {
		return errors.New("refusing hook input because the running Mimir binary differs from its receipt")
	}
	return nil
}

func (s Service) queue(delivery Delivery) error {
	data, err := json.Marshal(delivery)
	if err != nil {
		return err
	}
	dir := filepath.Join(s.Home, "hook-outbox")
	if err := s.cleanupDir(dir, outboxMaxAge); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var total int64
	for _, entry := range entries {
		if info, err := entry.Info(); err == nil && info.Mode().IsRegular() {
			total += info.Size()
		}
	}
	queuedBytes := len(data)
	if len(s.key) != 0 {
		queuedBytes = base64.RawStdEncoding.EncodedLen(len(data)+16) + 512
	}
	if len(entries) >= maxOutboxItems || total+int64(queuedBytes) > maxOutboxBytes {
		return errors.New("hook outbox is full")
	}
	sum := sha256.Sum256(data)
	suffix := fmt.Sprintf("-%x.json", sum[:16])
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), suffix) {
			return nil
		}
	}
	sequence, release, err := s.nextOutboxSequence()
	if err != nil {
		return err
	}
	defer release()
	path := filepath.Join(dir, fmt.Sprintf("%020d%s", sequence, suffix))
	return s.writeProtected(dir, path, data, "outbox")
}

func (s Service) nextOutboxSequence() (uint64, func(), error) {
	path := filepath.Join(s.Home, "hook-outbox.sequence")
	release, err := acquireFileLock(path+".lock", 2*time.Second)
	if err != nil {
		return 0, nil, err
	}
	var current uint64
	if data, err := os.ReadFile(path); err == nil {
		current, err = strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
		if err != nil {
			release()
			return 0, nil, errors.New("invalid hook outbox sequence")
		}
	} else if !os.IsNotExist(err) {
		release()
		return 0, nil, err
	}
	current++
	if err := writeAtomic(s.Home, path, []byte(strconv.FormatUint(current, 10)+"\n")); err != nil {
		release()
		return 0, nil, err
	}
	return current, release, nil
}

func (s Service) ensureDirs() error {
	for _, dir := range []string{s.Home, filepath.Join(s.Home, "hook-outbox"), filepath.Join(s.Home, "hook-state")} {
		if err := secureDir(dir); err != nil {
			return err
		}
	}
	return nil
}

func (s Service) statePath(session string) string {
	sum := sha256.Sum256([]byte(session))
	return filepath.Join(s.Home, "hook-state", hex.EncodeToString(sum[:16])+".json")
}

func (s Service) writeState(session string, state promptState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return s.writeProtected(filepath.Join(s.Home, "hook-state"), s.statePath(session), data, "state")
}

func (s Service) readState(session string) (promptState, error) {
	var state promptState
	path := s.statePath(session)
	data, legacy, err := s.readProtected(path, "state")
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, errors.New("decoding hook prompt state")
	}
	if at, err := time.Parse(time.RFC3339Nano, state.At); err != nil || s.now().Sub(at) > stateMaxAge {
		_ = os.Remove(path)
		return promptState{}, os.ErrNotExist
	}
	if legacy && len(s.key) != 0 {
		if err := s.writeProtected(filepath.Dir(path), path, data, "state"); err != nil {
			return promptState{}, err
		}
	}
	return state, nil
}

func storageKey(token string) []byte {
	if strings.TrimSpace(token) == "" {
		return nil
	}
	sum := sha256.Sum256([]byte("mimir-hook-storage-v1\x00" + token))
	return sum[:]
}

func loadStorageKey(home string) ([]byte, error) {
	if err := secureDir(home); err != nil {
		return nil, err
	}
	path := filepath.Join(home, "hook-storage.key")
	data, err := os.ReadFile(path)
	if err == nil {
		key, decodeErr := base64.RawStdEncoding.DecodeString(strings.TrimSpace(string(data)))
		if decodeErr != nil || len(key) != 32 {
			return nil, errors.New("invalid hook storage key")
		}
		return key, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	release, err := acquireFileLock(path+".lock", 2*time.Second)
	if err != nil {
		return nil, err
	}
	defer release()
	if data, err := os.ReadFile(path); err == nil {
		key, decodeErr := base64.RawStdEncoding.DecodeString(strings.TrimSpace(string(data)))
		if decodeErr != nil || len(key) != 32 {
			return nil, errors.New("invalid hook storage key")
		}
		return key, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	encoded := append([]byte(base64.RawStdEncoding.EncodeToString(key)), '\n')
	if err := writeAtomic(home, path, encoded); err != nil {
		return nil, err
	}
	return key, nil
}

func acquireFileLock(path string, timeout time.Duration) (func(), error) {
	deadline := time.Now().Add(timeout)
	for {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			if closeErr := file.Close(); closeErr != nil {
				_ = os.Remove(path)
				return nil, closeErr
			}
			return func() { _ = os.Remove(path) }, nil
		}
		info, statErr := os.Lstat(path)
		contention := os.IsExist(err) || (runtime.GOOS == "windows" && os.IsPermission(err))
		if !contention && os.IsNotExist(statErr) {
			return nil, err
		}
		if statErr != nil && !os.IsNotExist(statErr) {
			return nil, statErr
		}
		if statErr == nil {
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("refusing unsafe hook lock %s", path)
			}
			if info.ModTime().Before(time.Now().Add(-30 * time.Second)) {
				if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
					return nil, removeErr
				}
				continue
			}
		}
		if time.Now().After(deadline) {
			return nil, errors.New("timed out waiting for hook storage lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (s Service) writeProtected(root, path string, plaintext []byte, purpose string) error {
	data := append(bytes.Clone(plaintext), '\n')
	if len(s.key) != 0 {
		block, err := aes.NewCipher(s.key)
		if err != nil {
			return err
		}
		gcm, err := cipher.NewGCM(block)
		if err != nil {
			return err
		}
		nonce := make([]byte, gcm.NonceSize())
		if _, err := rand.Read(nonce); err != nil {
			return err
		}
		envelope := protectedFile{Version: 1, Nonce: base64.RawStdEncoding.EncodeToString(nonce), Ciphertext: base64.RawStdEncoding.EncodeToString(gcm.Seal(nil, nonce, plaintext, []byte(purpose)))}
		data, err = json.Marshal(envelope)
		if err != nil {
			return err
		}
		data = append(data, '\n')
	}
	return writeAtomic(root, path, data)
}

func (s Service) readProtected(path, purpose string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	var envelope protectedFile
	if json.Unmarshal(data, &envelope) != nil || envelope.Version == 0 {
		return bytes.TrimSpace(data), true, nil
	}
	if envelope.Version != 1 {
		return nil, false, errors.New("queued hook data uses an unsupported protection version")
	}
	nonce, err := base64.RawStdEncoding.DecodeString(envelope.Nonce)
	if err != nil {
		return nil, false, errors.New("decoding protected hook data")
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(envelope.Ciphertext)
	if err != nil {
		return nil, false, errors.New("decoding protected hook data")
	}
	if len(s.key) == 0 {
		return nil, false, errors.New("queued hook data cannot be decrypted without its local storage key")
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, false, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, false, err
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, []byte(purpose))
	if err != nil {
		return nil, false, errors.New("authenticating protected hook data")
	}
	return plaintext, false, nil
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s Service) cleanupStale() error {
	if err := s.cleanupDir(filepath.Join(s.Home, "hook-outbox"), outboxMaxAge); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := s.cleanupDir(filepath.Join(s.Home, "hook-state"), stateMaxAge); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s Service) cleanupDir(dir string, maxAge time.Duration) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	cutoff := s.now().Add(-maxAge)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() && info.ModTime().Before(cutoff) {
			if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}

func secureDir(path string) error {
	if symlink, err := containsSymlink(path); err != nil {
		return err
	} else if symlink {
		return fmt.Errorf("refusing symlinked hook directory %s", path)
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("refusing unsafe hook directory %s", path)
		}
		return os.Chmod(path, 0o700)
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.MkdirAll(path, 0o700)
}

func containsSymlink(path string) (bool, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false, err
	}
	root := filepath.VolumeName(abs) + string(filepath.Separator)
	rel := strings.TrimPrefix(abs, root)
	current := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return false, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if allowedDarwinFilesystemAlias(current) {
				continue
			}
			return true, nil
		}
	}
	return false, nil
}

func allowedDarwinFilesystemAlias(path string) bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	want, ok := map[string]string{
		string(filepath.Separator) + "etc": "private/etc",
		string(filepath.Separator) + "tmp": "private/tmp",
		string(filepath.Separator) + "var": "private/var",
	}[filepath.Clean(path)]
	if !ok {
		return false
	}
	target, err := os.Readlink(path)
	return err == nil && filepath.Clean(target) == want
}

func writeAtomic(root, path string, data []byte) error {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlinked hook file %s", path)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	temp, err := os.CreateTemp(root, ".mimir-hook-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func mimirHome() (string, error) {
	if home := strings.TrimSpace(os.Getenv("MIMIR_HOME")); home != "" {
		return filepath.Abs(home)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".mimir"), nil
}

func knownHarness(value string) bool {
	return value == "claude-code" || value == "codex" || value == "cursor"
}

func canonicalID(value string) string {
	if safeID.MatchString(value) {
		return value
	}
	sum := sha256.Sum256([]byte(value))
	return "h-" + hex.EncodeToString(sum[:16])
}

func boundedString(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return string([]byte(value)[:max])
}

func stringValue(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func firstString(body map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringValue(body[key]); value != "" {
			return value
		}
	}
	return ""
}
