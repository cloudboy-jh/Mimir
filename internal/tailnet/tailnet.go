// Package tailnet reports this machine's Tailscale identity and peer presence
// to the Worker. Reporting is best effort and silent when Tailscale is absent.
package tailnet

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ThrottleInterval bounds how often the hook path reports the tailnet.
const ThrottleInterval = 10 * time.Minute

const stampFile = "tailnet-reported"

type Self struct {
	HostName    string   `json:"host_name"`
	DNSName     string   `json:"dns_name"`
	TailnetName string   `json:"tailnet_name"`
	IPs         []string `json:"ips"`
}

type Peer struct {
	DNSName  string  `json:"dns_name"`
	HostName string  `json:"host_name"`
	Online   bool    `json:"online"`
	LastSeen *string `json:"last_seen"`
}

// Payload is the POST /machine/tailnet body.
type Payload struct {
	Self  Self   `json:"self"`
	Peers []Peer `json:"peers"`
}

// Requester is the subset of mimirapi.Client used for reporting.
type Requester interface {
	Request(ctx context.Context, method, path string, body any) ([]byte, error)
}

var errUnavailable = errors.New("tailscale unavailable")

var statusJSON = func(ctx context.Context) ([]byte, error) {
	binary, err := exec.LookPath("tailscale")
	if err != nil {
		return nil, errUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, binary, "status", "--json").Output()
}

// Parse converts `tailscale status --json` output into the Worker payload.
func Parse(data []byte) (Payload, error) {
	var status struct {
		Self *struct {
			HostName     string   `json:"HostName"`
			DNSName      string   `json:"DNSName"`
			TailscaleIPs []string `json:"TailscaleIPs"`
		} `json:"Self"`
		CurrentTailnet *struct {
			Name string `json:"Name"`
		} `json:"CurrentTailnet"`
		Peer map[string]struct {
			HostName string `json:"HostName"`
			DNSName  string `json:"DNSName"`
			Online   bool   `json:"Online"`
			LastSeen string `json:"LastSeen"`
		} `json:"Peer"`
	}
	if err := json.Unmarshal(data, &status); err != nil {
		return Payload{}, err
	}
	if status.Self == nil || dnsName(status.Self.DNSName) == "" {
		return Payload{}, errUnavailable
	}
	payload := Payload{
		Self: Self{
			HostName: status.Self.HostName, DNSName: dnsName(status.Self.DNSName),
			IPs: append([]string{}, status.Self.TailscaleIPs...),
		},
		Peers: []Peer{},
	}
	if status.CurrentTailnet != nil {
		payload.Self.TailnetName = status.CurrentTailnet.Name
	}
	for _, peer := range status.Peer {
		name := dnsName(peer.DNSName)
		if name == "" {
			continue
		}
		entry := Peer{DNSName: name, HostName: peer.HostName, Online: peer.Online}
		if seen, err := time.Parse(time.RFC3339Nano, peer.LastSeen); err == nil && !seen.IsZero() && seen.Year() > 1 {
			value := seen.UTC().Format(time.RFC3339)
			entry.LastSeen = &value
		}
		payload.Peers = append(payload.Peers, entry)
	}
	sort.Slice(payload.Peers, func(i, j int) bool { return payload.Peers[i].DNSName < payload.Peers[j].DNSName })
	return payload, nil
}

func dnsName(value string) string {
	return strings.TrimSuffix(strings.TrimSpace(value), ".")
}

// Report collects the tailnet state and posts it. It returns false without an
// error when Tailscale is not installed, not running, or not logged in.
func Report(ctx context.Context, api Requester) (bool, error) {
	data, err := statusJSON(ctx)
	if err != nil {
		return false, nil
	}
	payload, err := Parse(data)
	if err != nil {
		return false, nil
	}
	if _, err := api.Request(ctx, http.MethodPost, "/machine/tailnet", payload); err != nil {
		return false, err
	}
	return true, nil
}

// ReportThrottled reports at most once per ThrottleInterval, tracked by a
// timestamp file in home. Failures are ignored; it never blocks a hook.
func ReportThrottled(ctx context.Context, api Requester, home string, now time.Time) {
	stamp := filepath.Join(home, stampFile)
	if info, err := os.Stat(stamp); err == nil && now.Sub(info.ModTime()) < ThrottleInterval {
		return
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return
	}
	if err := os.WriteFile(stamp, []byte(now.UTC().Format(time.RFC3339)+"\n"), 0o600); err != nil {
		return
	}
	_ = os.Chtimes(stamp, now, now)
	_, _ = Report(ctx, api)
}
