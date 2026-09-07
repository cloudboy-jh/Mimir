package sessionimport

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeOMPFixture(t *testing.T, root, name, id, parent string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil { t.Fatal(err) }
	header, err := json.Marshal(ompHeader{Type: "session", ID: id, ParentSessionID: parent, Timestamp: "2026-08-20T10:00:00Z"})
	if err != nil { t.Fatal(err) }
	// Messages are deliberately unreadable: relationship discovery needs only
	// metadata, never conversations or an OMP exchange replay.
	data := "{\"type\":\"title\",\"title\":\"Saved title\"}\n" + string(header) + "\nnot a transcript\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil { t.Fatal(err) }
}

func TestOMPDiscoveryResolvesImmediateParentBeforeSelection(t *testing.T) {
	root := t.TempDir()
	writeOMPFixture(t, root, "root.jsonl", "root-uuid", "")
	writeOMPFixture(t, root, "root/Alpha.jsonl", "alpha-uuid", "")
	writeOMPFixture(t, root, "root/Alpha/Alpha.Beta.jsonl", "beta-uuid", "")
	writeOMPFixture(t, root, "root/Alpha.Gamma.jsonl", "gamma-uuid", "")
	adapter := NewOhMyPiAdapter(root)
	for _, id := range []string{"beta-uuid", "gamma-uuid"} {
		sessions, err := adapter.DiscoverWithOptions(context.Background(), Options{SourceIDs: []string{id}})
		if err != nil { t.Fatal(err) }
		if len(sessions) != 1 || sessions[0].ParentSessionID != "alpha-uuid" || sessions[0].ParentLinkStatus != "pending" {
			t.Fatalf("selected %s: %#v", id, sessions)
		}
		if sessions[0].Title != "Saved title" || len(sessions[0].Exchanges) != 0 { t.Fatalf("metadata = %#v", sessions[0]) }
	}
}

func TestOMPDiscoveryRefusesMissingPrefixAndCopiedIDs(t *testing.T) {
	root := t.TempDir()
	writeOMPFixture(t, root, "root.jsonl", "root-uuid", "")
	writeOMPFixture(t, root, "root/Missing.Child.jsonl", "orphan-uuid", "")
	writeOMPFixture(t, root, "root/Alpha.jsonl", "copied-uuid", "")
	writeOMPFixture(t, root, "root/Other.jsonl", "copied-uuid", "")
	writeOMPFixture(t, root, "root/Alpha.Child.jsonl", "child-uuid", "")
	sessions, err := NewOhMyPiAdapter(root).Discover(context.Background())
	if err != nil { t.Fatal(err) }
	for _, session := range sessions {
		if session.ParentLinkStatus != "unresolved" || session.ParentSessionID != "" || session.ParentLinkReason == "" {
			t.Fatalf("ambiguous ancestry was accepted: %#v", session)
		}
	}
}

func TestOMPDiscoveryRejectsHeaderCyclesWithoutFilenameFallback(t *testing.T) {
	root := t.TempDir()
	writeOMPFixture(t, root, "a.jsonl", "a", "b")
	writeOMPFixture(t, root, "b.jsonl", "b", "a")
	if err := os.WriteFile(filepath.Join(root, "no-header.jsonl"), []byte("{\"type\":\"message\",\"id\":\"not-a-session\"}\n"), 0600); err != nil { t.Fatal(err) }
	sessions, err := NewOhMyPiAdapter(root).Discover(context.Background())
	if err != nil { t.Fatal(err) }
	for _, session := range sessions {
		if session.ParentLinkStatus != "unresolved" { t.Fatalf("unsafe ancestry = %#v", session) }
		if session.ID == "not-a-session" || session.ID == "no-header" { t.Fatalf("invented ID = %#v", session) }
	}
}

func TestOMPDiscoveryFailsClosedAtFileBound(t *testing.T) {
	root := t.TempDir()
	writeOMPFixture(t, root, "root.jsonl", "root", "")
	writeOMPFixture(t, root, "root/Child.jsonl", "child", "")
	sessions, err := (OhMyPiAdapter{Roots: []string{root}, MaxFiles: 1}).Discover(context.Background())
	if err == nil || !strings.Contains(err.Error(), "exceeds limit") || len(sessions) != 0 {
		t.Fatalf("bounded discovery sessions=%#v err=%v", sessions, err)
	}
}
