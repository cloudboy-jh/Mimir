package deployment

import (
	"context"
	"fmt"
	"os"
	"strings"
)

type wranglerAccountContextKey struct{}

func withWranglerAccount(ctx context.Context, accountID string) context.Context {
	return context.WithValue(ctx, wranglerAccountContextKey{}, accountID)
}

func wranglerAccountID(ctx context.Context) string {
	accountID, _ := ctx.Value(wranglerAccountContextKey{}).(string)
	return strings.TrimSpace(accountID)
}

func (s *Service) selectDeploymentAccount(opts Options, identity Identity) (string, error) {
	explicit := strings.TrimSpace(opts.AccountID)
	if explicit != "" {
		return validateAccount(identity, explicit, "--account-id")
	}
	if accountID := strings.TrimSpace(os.Getenv("CLOUDFLARE_ACCOUNT_ID")); accountID != "" {
		return validateAccount(identity, accountID, "CLOUDFLARE_ACCOUNT_ID")
	}
	if s.LoadState != nil {
		if state, err := s.LoadState(); err == nil && deploymentStateSuitable(state, opts) && identityHasAccount(identity, state.AccountID) {
			return strings.TrimSpace(state.AccountID), nil
		}
	}
	if len(identity.Accounts) == 1 {
		return strings.TrimSpace(identity.Accounts[0].ID), nil
	}
	if len(identity.Accounts) == 0 {
		return "", fmt.Errorf("no Cloudflare accounts are available for the authenticated user; run wrangler login")
	}
	return "", fmt.Errorf("multiple Cloudflare accounts are authenticated (%s); select one with --account-id or CLOUDFLARE_ACCOUNT_ID", accountSummary(identity))
}

func validateAccount(identity Identity, accountID, source string) (string, error) {
	if identityHasAccount(identity, accountID) {
		return accountID, nil
	}
	return "", fmt.Errorf("Cloudflare account %q selected by %s is not among the authenticated accounts (%s); choose an authenticated account or run wrangler login", accountID, source, accountSummary(identity))
}

func identityHasAccount(identity Identity, accountID string) bool {
	accountID = strings.TrimSpace(accountID)
	for _, account := range identity.Accounts {
		if strings.TrimSpace(account.ID) == accountID {
			return true
		}
	}
	return false
}

func deploymentStateSuitable(state DeploymentState, opts Options) bool {
	if strings.TrimSpace(state.AccountID) == "" {
		return false
	}
	return compatibleOption(opts.WorkerName, state.WorkerName) &&
		compatibleOption(opts.DatabaseName, state.DatabaseName) &&
		compatibleOption(opts.BucketName, state.BucketName)
}

func compatibleOption(requested, saved string) bool {
	return strings.TrimSpace(requested) == "" || strings.TrimSpace(requested) == strings.TrimSpace(saved)
}

func accountSummary(identity Identity) string {
	accounts := make([]string, 0, len(identity.Accounts))
	for _, account := range identity.Accounts {
		name, id := strings.TrimSpace(account.Name), strings.TrimSpace(account.ID)
		if name == "" {
			accounts = append(accounts, id)
		} else {
			accounts = append(accounts, fmt.Sprintf("%s [%s]", name, id))
		}
	}
	if len(accounts) == 0 {
		return "none"
	}
	return strings.Join(accounts, ", ")
}
