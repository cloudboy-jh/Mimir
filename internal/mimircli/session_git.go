package mimircli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/mimirapi"
	"github.com/cloudboy-jh/mimir/internal/sessionimport"
)

var captureSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)
var artifactDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

const sessionGitUsage = "usage: mimir session git capture <id> <full-sha> [--json] | mimir session git repair <id> <full-sha> --expected-digest <old-sha256> [--json] | mimir session git verify <id> <full-sha> [--json]"

func cmdSessionGit(ctx context.Context, args []string, out io.Writer) error {
	jsonOutput := len(args) > 0 && args[len(args)-1] == "--json"
	if jsonOutput {
		args = args[:len(args)-1]
	}
	if len(args) < 3 || args[1] == "" || strings.HasPrefix(args[1], "-") || strings.ContainsAny(args[1], "\r\n\x00") {
		return errors.New(sessionGitUsage)
	}
	if (args[0] == "capture" || args[0] == "verify") && len(args) != 3 ||
		args[0] == "repair" && (len(args) != 5 || args[3] != "--expected-digest") ||
		(args[0] != "capture" && args[0] != "verify" && args[0] != "repair") {
		return errors.New(sessionGitUsage)
	}
	id, sha := args[1], args[2]
	if !captureSHA.MatchString(sha) {
		return errors.New("commit must be a lowercase full 40-character SHA")
	}
	if args[0] == "repair" && !artifactDigest.MatchString(args[4]) {
		return errors.New("expected digest must be a lowercase full 64-character SHA256")
	}
	pointer, err := loadPointer()
	if err != nil {
		return err
	}
	client := mimirapi.Client{HTTPClient: httpClient, Pointer: pointer}
	if args[0] == "verify" {
		verification, err := verifySessionGit(ctx, client, id, sha)
		if err != nil {
			return err
		}
		return printGitVerification(out, verification, nil, jsonOutput)
	}
	data, err := client.Request(ctx, http.MethodGet, "/sessions/"+url.PathEscape(id)+"/status", nil)
	if err != nil {
		return errors.New("cannot verify session existence")
	}
	var status struct {
		SessionID string `json:"session_id"`
	}
	if json.Unmarshal(data, &status) != nil || status.SessionID != id {
		return errors.New("session status does not match requested session")
	}
	artifact, err := (sessionimport.CheckoutArtifactCollector{}).CollectCommit(ctx, ".", sha)
	if err != nil {
		return err
	}
	if args[0] == "repair" {
		response, err := client.Request(ctx, http.MethodPost, "/sessions/"+url.PathEscape(id)+"/git-artifacts/"+sha+"/repair", struct {
			ExpectedDigest string                    `json:"expected_digest"`
			Artifact       sessionimport.GitArtifact `json:"artifact"`
		}{args[4], artifact})
		if err != nil {
			return fmt.Errorf("Git artifact repair failed: %w", err)
		}
		var result struct {
			Kind      string `json:"kind"`
			SessionID string `json:"session_id"`
			Artifacts []struct {
				CommitSHA     string `json:"commit_sha"`
				CaptureStatus string `json:"capture_status"`
				PatchSHA256   string `json:"patch_sha256"`
			} `json:"artifacts"`
			Repaired struct {
				CommitSHA      string `json:"commit_sha"`
				PreviousDigest string `json:"previous_digest"`
				PatchSHA256    string `json:"patch_sha256"`
				AuditR2Key     string `json:"audit_r2_key"`
			} `json:"repaired"`
		}
		if json.Unmarshal(response, &result) != nil || result.Kind != "ok" || result.SessionID != id ||
			len(result.Artifacts) != 1 || result.Artifacts[0].CommitSHA != sha ||
			result.Artifacts[0].CaptureStatus != "saved" || !artifactDigest.MatchString(result.Artifacts[0].PatchSHA256) ||
			result.Repaired.CommitSHA != sha || result.Repaired.PreviousDigest != args[4] ||
			result.Repaired.PatchSHA256 != result.Artifacts[0].PatchSHA256 || strings.TrimSpace(result.Repaired.AuditR2Key) == "" {
			return errors.New("invalid Git artifact repair response")
		}
		verification, err := verifySessionGit(ctx, client, id, sha)
		if err != nil {
			return fmt.Errorf("repair submitted but stored patch verification failed: %w", err)
		}
		if verification.PatchSHA256 != result.Artifacts[0].PatchSHA256 {
			return errors.New("repair submitted but stored patch digest differs from repair response")
		}
		return printGitVerification(out, verification, response, jsonOutput)
	}
	result, err := sessionimport.New(client).UploadGitArtifacts(ctx, sessionimport.Session{ID: id}, []sessionimport.GitArtifact{artifact})
	if err != nil {
		return errors.New("Git artifact upload failed")
	}
	if jsonOutput {
		return json.NewEncoder(out).Encode(struct {
			SessionID string `json:"session_id"`
			CommitSHA string `json:"commit_sha"`
			sessionimport.GitArtifactUpload
		}{id, sha, result})
	}
	_, err = fmt.Fprintf(out, "Git commit %s: %d saved, %d already present (session %s)\n", sha, result.ArtifactsSaved, result.ArtifactsDuplicate, id)
	return err
}

type gitVerification struct {
	SessionID      string `json:"session_id"`
	CommitSHA      string `json:"commit_sha"`
	Verified       bool   `json:"verified"`
	PatchSHA256    string `json:"patch_sha256"`
	PatchBytes     int    `json:"patch_bytes"`
	PatchFiles     int    `json:"patch_files"`
	PatchAdditions int    `json:"patch_additions"`
	PatchDeletions int    `json:"patch_deletions"`
	PatchLines     int    `json:"patch_lines"`
}

func verifySessionGit(ctx context.Context, client mimirapi.Client, id, sha string) (gitVerification, error) {
	result := gitVerification{SessionID: id, CommitSHA: sha}
	path := "/sessions/" + url.PathEscape(id)
	data, err := client.Request(ctx, http.MethodGet, path, nil)
	if err != nil {
		return result, fmt.Errorf("cannot fetch canonical session detail: %w", err)
	}
	var detail struct {
		Session struct {
			ID string `json:"id"`
		} `json:"session"`
		Artifacts []struct {
			CommitSHA     string `json:"commit_sha"`
			CaptureStatus string `json:"capture_status"`
			Digest        string `json:"patch_sha256"`
			Bytes         int    `json:"patch_bytes"`
			Files         int    `json:"patch_files"`
			Additions     int    `json:"patch_additions"`
			Deletions     int    `json:"patch_deletions"`
		} `json:"git_artifacts"`
	}
	if json.Unmarshal(data, &detail) != nil || detail.Session.ID != id {
		return result, errors.New("canonical session detail does not match requested session")
	}
	index := -1
	for i, artifact := range detail.Artifacts {
		if artifact.CommitSHA == sha {
			if index != -1 {
				return result, errors.New("duplicate canonical Git artifact")
			}
			index = i
		}
	}
	if index == -1 {
		return result, errors.New("Git artifact not found; use mimir session git capture with the exact ID and full SHA")
	}
	artifact := detail.Artifacts[index]
	if artifact.CaptureStatus != "saved" || !artifactDigest.MatchString(artifact.Digest) {
		return result, errors.New("Git artifact is not saved with a valid patch digest")
	}
	patch, err := client.Request(ctx, http.MethodGet, path+"/git-artifacts/"+sha+"/patch", nil)
	if err != nil {
		return result, fmt.Errorf("cannot fetch stored patch: %w", err)
	}
	result.PatchSHA256 = fmt.Sprintf("%x", sha256.Sum256(patch))
	result.PatchBytes = len(patch)
	lines := strings.Split(string(patch), "\n")
	result.PatchLines = strings.Count(string(patch), "\n")
	if len(patch) > 0 && patch[len(patch)-1] != '\n' {
		result.PatchLines++
	}
	format := false
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			result.PatchFiles++
			format = true
		case strings.HasPrefix(line, "diff --cc "), strings.HasPrefix(line, "diff --combined "):
			format = true
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			result.PatchAdditions++
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			result.PatchDeletions++
		}
	}
	if result.PatchSHA256 != artifact.Digest {
		return result, fmt.Errorf("stored patch SHA256 mismatch (expected %s, got %s)", artifact.Digest, result.PatchSHA256)
	}
	if !format || result.PatchLines < 2 || strings.ContainsRune(string(patch), '\x00') {
		return result, fmt.Errorf("stored patch is not a multiline Git patch; repair with --expected-digest %s", artifact.Digest)
	}
	if result.PatchBytes != artifact.Bytes || result.PatchFiles != artifact.Files || result.PatchAdditions != artifact.Additions || result.PatchDeletions != artifact.Deletions {
		return result, errors.New("stored patch statistics do not match canonical artifact metadata")
	}
	result.Verified = true
	return result, nil
}

func printGitVerification(out io.Writer, verification gitVerification, repair json.RawMessage, jsonOutput bool) error {
	if jsonOutput {
		return json.NewEncoder(out).Encode(struct {
			gitVerification
			Repair json.RawMessage `json:"repair,omitempty"`
		}{verification, repair})
	}
	_, err := fmt.Fprintf(out, "Verified Git patch %s (session %s): SHA256 %s, %d bytes, %d lines, %d files, +%d/-%d\n", verification.CommitSHA, verification.SessionID, verification.PatchSHA256, verification.PatchBytes, verification.PatchLines, verification.PatchFiles, verification.PatchAdditions, verification.PatchDeletions)
	return err
}
