package deployment

import (
	"context"
	"strings"
	"testing"
)

func TestSelectDeploymentAccountPrecedence(t *testing.T) {
	identity := testIdentity(
		[2]string{"explicit", "Explicit"},
		[2]string{"environment", "Environment"},
		[2]string{"saved", "Saved"},
	)
	tests := []struct {
		name      string
		explicit  string
		environ   string
		state     DeploymentState
		want      string
		wantError string
	}{
		{name: "explicit", explicit: "explicit", environ: "environment", state: DeploymentState{AccountID: "saved"}, want: "explicit"},
		{name: "environment", environ: "environment", state: DeploymentState{AccountID: "saved"}, want: "environment"},
		{name: "saved", state: DeploymentState{AccountID: "saved", DatabaseName: "mimir"}, want: "saved"},
		{name: "unsuitable saved state", state: DeploymentState{AccountID: "saved", DatabaseName: "other"}, wantError: "multiple Cloudflare accounts"},
		{name: "explicit mismatch", explicit: "missing", wantError: "selected by --account-id is not among the authenticated accounts"},
		{name: "environment mismatch", environ: "missing", wantError: "selected by CLOUDFLARE_ACCOUNT_ID is not among the authenticated accounts"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CLOUDFLARE_ACCOUNT_ID", test.environ)
			service := NewService(nil)
			service.LoadState = func() (DeploymentState, error) { return test.state, nil }
			got, err := service.selectDeploymentAccount(Options{AccountID: test.explicit, DatabaseName: "mimir"}, identity)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("error = %v, want containing %q", err, test.wantError)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("account = %q, error = %v", got, err)
			}
		})
	}
}

func TestSelectDeploymentAccountUsesSoleAuthenticatedAccount(t *testing.T) {
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "")
	service := NewService(nil)
	service.LoadState = func() (DeploymentState, error) { return DeploymentState{}, nil }
	got, err := service.selectDeploymentAccount(Options{}, testIdentity([2]string{"sole", "Only Account"}))
	if err != nil || got != "sole" {
		t.Fatalf("account = %q, error = %v", got, err)
	}
}

func TestWranglerAccountContext(t *testing.T) {
	ctx := withWranglerAccount(context.Background(), " account-1 ")
	if got := wranglerAccountID(ctx); got != "account-1" {
		t.Fatalf("account = %q", got)
	}
}

func testIdentity(accounts ...[2]string) Identity {
	identity := Identity{LoggedIn: true}
	for _, account := range accounts {
		identity.Accounts = append(identity.Accounts, struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}{ID: account[0], Name: account[1]})
	}
	return identity
}
