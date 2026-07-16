package mimircli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

type reconcilePage struct {
	Scanned        int    `json:"scanned"`
	DatabaseCursor string `json:"database_cursor"`
	Finalized      struct {
		ExchangeIDs []string `json:"exchange_ids"`
	} `json:"finalized"`
	Pending struct {
		ExchangeIDs      []string `json:"exchange_ids"`
		StaleExchangeIDs []string `json:"stale_exchange_ids"`
	} `json:"pending"`
	MissingSaved struct {
		ExchangeIDs []string `json:"exchange_ids"`
		SessionIDs  []string `json:"session_ids"`
	} `json:"missing_saved"`
	Orphans struct {
		R2Keys []string `json:"r2_keys"`
		Cursor string   `json:"cursor"`
	} `json:"orphans"`
}

type reconcileReport struct {
	Pages        int      `json:"pages"`
	Scanned      int      `json:"scanned"`
	Finalized    []string `json:"finalized_exchange_ids"`
	Pending      []string `json:"pending_exchange_ids"`
	StalePending []string `json:"stale_pending_exchange_ids"`
	MissingSaved []string `json:"missing_saved_exchange_ids"`
	Affected     []string `json:"affected_session_ids"`
	Orphans      []string `json:"orphan_r2_keys"`
}

func runReconcile(ctx context.Context) ([]byte, error) {
	const pageLimit = 100
	report := reconcileReport{
		Finalized:    []string{},
		Pending:      []string{},
		StalePending: []string{},
		MissingSaved: []string{},
		Affected:     []string{},
		Orphans:      []string{},
	}
	databaseCursor, r2Cursor := "", ""
	scanDatabase, scanR2 := true, true
	for scanDatabase || scanR2 {
		if report.Pages >= 1000 {
			return nil, fmt.Errorf("reconciliation exceeded 1000 pages")
		}
		query := url.Values{"limit": {fmt.Sprint(pageLimit)}}
		if databaseCursor != "" {
			query.Set("database_cursor", databaseCursor)
		}
		if r2Cursor != "" {
			query.Set("cursor", r2Cursor)
		}
		if !scanDatabase {
			query.Set("scan_database", "false")
		}
		if !scanR2 {
			query.Set("scan_r2", "false")
		}
		data, err := remoteRequest(ctx, "POST", "/reconcile?"+query.Encode(), nil)
		if err != nil {
			return nil, err
		}
		var page reconcilePage
		if err := json.Unmarshal(data, &page); err != nil {
			return nil, fmt.Errorf("decoding reconciliation response: %w", err)
		}
		report.Pages++
		report.Scanned += page.Scanned
		report.Finalized = appendUnique(report.Finalized, page.Finalized.ExchangeIDs...)
		report.Pending = appendUnique(report.Pending, page.Pending.ExchangeIDs...)
		report.StalePending = appendUnique(report.StalePending, page.Pending.StaleExchangeIDs...)
		report.MissingSaved = appendUnique(report.MissingSaved, page.MissingSaved.ExchangeIDs...)
		report.Affected = appendUnique(report.Affected, page.MissingSaved.SessionIDs...)
		report.Orphans = appendUnique(report.Orphans, page.Orphans.R2Keys...)
		databaseCursor = page.DatabaseCursor
		r2Cursor = page.Orphans.Cursor
		scanDatabase = databaseCursor != ""
		scanR2 = r2Cursor != ""
	}
	return json.Marshal(report)
}

func appendUnique(values []string, additions ...string) []string {
	seen := make(map[string]struct{}, len(values)+len(additions))
	for _, value := range values {
		seen[value] = struct{}{}
	}
	for _, value := range additions {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values
}
