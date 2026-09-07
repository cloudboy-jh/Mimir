package sessionimport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

type parentRecord struct {
	SessionID string `json:"session_id"`
	ParentSessionID string `json:"parent_session_id"`
	App string `json:"app"`
}

func (s Service) ompParentRecord(ctx context.Context, id string) (parentRecord, error) {
	data, err := s.Client.RequestWithHeaders(ctx, http.MethodGet, "/sessions/"+url.PathEscape(id)+"/parent", nil, nil)
	if err != nil { return parentRecord{}, err }
	var record parentRecord
	if err := json.Unmarshal(data, &record); err != nil { return parentRecord{}, err }
	if record.SessionID != id || record.App != "oh-my-pi" {
		return parentRecord{}, fmt.Errorf("canonical record %s is not an exact oh-my-pi session", id)
	}
	return record, nil
}

func (s Service) repairOMPParent(ctx context.Context, session Session) SessionReport {
	report := SessionReport{Source: session.Harness, SourceID: session.SourceID, SessionID: session.ID, Status: "skipped", ParentSessionID: session.ParentSessionID, ParentLinkStatus: session.ParentLinkStatus, ParentLinkReason: session.ParentLinkReason}
	if session.ParentLinkStatus != "pending" || session.ID == "" || session.ParentSessionID == "" {
		return report
	}
	child, err := s.ompParentRecord(ctx, session.ID)
	if err != nil {
		report.ParentLinkStatus, report.ParentLinkReason = "unresolved", "canonical child unavailable: " + err.Error()
		return report
	}
	if child.ParentSessionID != "" && child.ParentSessionID != session.ParentSessionID {
		report.ParentLinkStatus, report.ParentLinkReason = "conflict", "existing parent differs; unchanged"
		return report
	}
	if _, err := s.ompParentRecord(ctx, session.ParentSessionID); err != nil {
		report.ParentLinkStatus, report.ParentLinkReason = "unresolved", "canonical parent unavailable: " + err.Error()
		return report
	}
	if child.ParentSessionID == session.ParentSessionID {
		report.ParentLinkStatus, report.ParentLinkReason = "unchanged", "exact parent already linked"
		return report
	}
	data, err := s.Client.RequestWithHeaders(ctx, http.MethodPatch, "/sessions/"+url.PathEscape(session.ID)+"/parent", map[string]string{"parent_session_id": session.ParentSessionID}, nil)
	if err != nil {
		report.Status, report.Error = "failed", "repairing parent: " + err.Error()
		report.ParentLinkStatus, report.ParentLinkReason = "unresolved", "parent update not confirmed"
		return report
	}
	var updated parentRecord
	if err := json.Unmarshal(data, &updated); err != nil || updated.SessionID != session.ID || updated.ParentSessionID != session.ParentSessionID {
		report.Status, report.Error = "failed", "parent update returned an invalid receipt"
		report.ParentLinkStatus, report.ParentLinkReason = "unresolved", "parent update not confirmed"
		return report
	}
	report.Status = "imported"
	report.ParentLinkStatus, report.ParentLinkReason = "linked", "exact parent linked; exchanges and lifecycle unchanged"
	return report
}
