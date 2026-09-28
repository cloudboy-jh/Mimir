package mimircli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"

	"github.com/cloudboy-jh/mimir/internal/mimirapi"
	"github.com/cloudboy-jh/mimir/internal/sessionimport"
)

var captureSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

func cmdSessionGit(ctx context.Context, args []string, out io.Writer) error {
	const usage = "usage: mimir session git capture <session-id> <full-commit-sha> [--json]"
	if len(args) < 3 || args[0] != "capture" || args[1] == "" || args[1][0] == '-' ||
		(len(args) != 3 && (len(args) != 4 || args[3] != "--json")) {
		return errors.New(usage)
	}
	id, sha := args[1], args[2]
	if !captureSHA.MatchString(sha) {
		return errors.New("commit must be a lowercase full 40-character SHA")
	}
	pointer, err := loadPointer()
	if err != nil {
		return err
	}
	client := mimirapi.Client{HTTPClient: httpClient, Pointer: pointer}
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
	result, err := sessionimport.New(client).UploadGitArtifacts(ctx, sessionimport.Session{ID: id}, []sessionimport.GitArtifact{artifact})
	if err != nil {
		return errors.New("Git artifact upload failed")
	}
	if len(args) == 4 {
		return json.NewEncoder(out).Encode(struct {
			SessionID string `json:"session_id"`
			CommitSHA string `json:"commit_sha"`
			sessionimport.GitArtifactUpload
		}{id, sha, result})
	}
	_, err = fmt.Fprintf(out, "Git commit %s: %d saved, %d already present (session %s)\n", sha, result.ArtifactsSaved, result.ArtifactsDuplicate, id)
	return err
}
