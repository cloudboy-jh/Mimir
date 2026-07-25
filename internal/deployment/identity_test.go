package deployment

import (
	"testing"

	"github.com/cloudboy-jh/mimir/internal/mimirapi"
)

func TestIdentityStoreRoundTrip(t *testing.T) {
	t.Setenv(mimirapi.EnvHome, t.TempDir())
	want := Identity{LoggedIn: true, AuthType: "OAuth Token", Email: "user@example.com"}
	if err := SaveIdentity(want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if got.LoggedIn != want.LoggedIn || got.AuthType != want.AuthType || got.Email != want.Email {
		t.Fatalf("identity %#v", got)
	}
}
