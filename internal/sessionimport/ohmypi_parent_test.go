package sessionimport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

type historicalRecord struct {
	parentRecord
	StartedAt string
	LastActive string
	State string
	Outcome string
	Exchanges int
}

type parentRepairClient struct {
	records map[string]historicalRecord
	writes int
}

func (c *parentRepairClient) RequestWithHeaders(_ context.Context, method, path string, body any, _ http.Header) ([]byte, error) {
	if !strings.HasSuffix(path, "/parent") { return nil, fmt.Errorf("unexpected history mutation: %s %s", method, path) }
	id := strings.TrimSuffix(strings.TrimPrefix(path, "/sessions/"), "/parent")
	record, ok := c.records[id]
	if !ok { return nil, fmt.Errorf("record %s absent", id) }
	if method == http.MethodGet { return json.Marshal(record.parentRecord) }
	if method != http.MethodPatch { return nil, fmt.Errorf("unexpected method %s", method) }
	parent := body.(map[string]string)["parent_session_id"]
	if record.ParentSessionID != "" && record.ParentSessionID != parent { return nil, fmt.Errorf("parent conflict") }
	record.ParentSessionID = parent
	c.records[id] = record
	c.writes++
	return json.Marshal(record.parentRecord)
}

func TestOMPRepairPreservesHistoryAndIsIdempotent(t *testing.T) {
	root := historicalRecord{parentRecord: parentRecord{SessionID: "root", App: "oh-my-pi"}, StartedAt: "old", LastActive: "old", State: "ended", Outcome: "landed", Exchanges: 12}
	child := historicalRecord{parentRecord: parentRecord{SessionID: "child", App: "oh-my-pi"}, StartedAt: "earlier", LastActive: "later", State: "ended", Outcome: "discarded", Exchanges: 3}
	client := &parentRepairClient{records: map[string]historicalRecord{"root": root, "child": child}}
	service := Service{Client: client}
	session := Session{ID: "child", SourceID: "child", Harness: "oh-my-pi", ParentSessionID: "root", ParentLinkStatus: "pending", Exchanges: []Exchange{{ExchangeID: "would-duplicate"}}}
	first, err := service.Upload(context.Background(), []Session{session})
	if err != nil { t.Fatal(err) }
	if first.Sessions[0].ParentLinkStatus != "linked" || first.ExchangesUploaded != 0 { t.Fatalf("first = %#v", first) }
	second, err := service.Upload(context.Background(), []Session{session})
	if err != nil { t.Fatal(err) }
	if second.Sessions[0].ParentLinkStatus != "unchanged" || client.writes != 1 { t.Fatalf("second = %#v, writes=%d", second, client.writes) }
	child.ParentSessionID = "root"
	if !reflect.DeepEqual(client.records, map[string]historicalRecord{"root": root, "child": child}) {
		t.Fatalf("non-parent history changed: %#v", client.records)
	}
}

func TestOMPRepairLeavesAbsentConflictingAndOtherHarnessRecordsUnchanged(t *testing.T) {
	cases := []struct { name string; records map[string]historicalRecord; status string }{
		{"absent-parent", map[string]historicalRecord{"child": {parentRecord: parentRecord{SessionID: "child", App: "oh-my-pi"}}}, "unresolved"},
		{"other-harness", map[string]historicalRecord{"child": {parentRecord: parentRecord{SessionID: "child", App: "pi"}}, "root": {parentRecord: parentRecord{SessionID: "root", App: "oh-my-pi"}}}, "unresolved"},
		{"conflict", map[string]historicalRecord{"child": {parentRecord: parentRecord{SessionID: "child", App: "oh-my-pi", ParentSessionID: "existing"}}, "root": {parentRecord: parentRecord{SessionID: "root", App: "oh-my-pi"}}}, "conflict"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			client := &parentRepairClient{records: test.records}
			report, err := (Service{Client: client}).Upload(context.Background(), []Session{{ID: "child", Harness: "oh-my-pi", ParentSessionID: "root", ParentLinkStatus: "pending"}})
			if err != nil { t.Fatal(err) }
			if client.writes != 0 || report.Sessions[0].ParentLinkStatus != test.status || report.Sessions[0].ParentLinkReason == "" { t.Fatalf("report = %#v, writes=%d", report, client.writes) }
		})
	}
}

func TestAllSourceDiscoveryDoesNotReplayOMPAsPi(t *testing.T) {
	root := t.TempDir()
	writeOMPFixture(t, root, "session.jsonl", "shared-id", "")
	service := Service{Sources: []Source{namedSource{name: "pi", sessions: []Session{{ID: "shared-id", SourceID: "shared-id", Harness: "pi", Exchanges: []Exchange{{ExchangeID: "pi-duplicate"}}}}}, NewOhMyPiAdapter(root)}}
	discovery, err := service.Discover(context.Background(), Options{})
	if err != nil { t.Fatal(err) }
	if len(discovery.Sessions) != 1 || discovery.Sessions[0].Harness != "oh-my-pi" || len(discovery.Sessions[0].Exchanges) != 0 { t.Fatalf("discovery = %#v", discovery) }
}

type namedSource struct { name string; sessions []Session }
func (s namedSource) Name() string { return s.name }
func (s namedSource) Discover(context.Context) ([]Session, error) { return s.sessions, nil }
