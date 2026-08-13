package mimircli

import "testing"

func TestParseDeployOptionsAccountID(t *testing.T) {
	opts, err := parseDeployOptions([]string{"--account-id", "account-123", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.AccountID != "account-123" || !opts.JSON {
		t.Fatalf("options = %#v", opts)
	}
}

func TestParseDeployOptionsAccountIDRequiresValue(t *testing.T) {
	for _, args := range [][]string{{"--account-id"}, {"--account-id", " "}} {
		if _, err := parseDeployOptions(args); err == nil {
			t.Fatalf("invalid account ID was accepted: %q", args)
		}
	}
}
