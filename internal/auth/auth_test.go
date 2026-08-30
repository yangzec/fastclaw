package auth

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/fastclaw-ai/fastclaw/internal/store"
	"github.com/fastclaw-ai/fastclaw/internal/users"
)

func TestSwitchToAppUserAddsSelectedAppUserAgentsToUserKeyScope(t *testing.T) {
	ctx := context.Background()
	st, err := store.NewDBStore(
		"sqlite",
		"file:"+filepath.Join(t.TempDir(), "fastclaw.db")+"?cache=shared",
	)
	if err != nil {
		t.Fatalf("NewDBStore: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	accounts, err := users.NewAccounts(st)
	if err != nil {
		t.Fatalf("NewAccounts: %v", err)
	}
	owner, err := accounts.Create(ctx, users.CreateInput{
		Username: "site-owner",
		Email:    "site-owner@example.com",
		Password: "test-password",
		Role:     users.RoleUser,
	})
	if err != nil {
		t.Fatalf("Create owner: %v", err)
	}
	keys, err := users.NewAPIKeys(st)
	if err != nil {
		t.Fatalf("NewAPIKeys: %v", err)
	}
	key, token, err := keys.Create(ctx, owner.ID, "site-key", users.APIKeyTypeUser, nil)
	if err != nil {
		t.Fatalf("Create API key: %v", err)
	}
	appUserA, err := accounts.EnsureAppUser(ctx, owner.ID, "end-user-a", "", key.ID)
	if err != nil {
		t.Fatalf("EnsureAppUser(A): %v", err)
	}
	appUserB, err := accounts.EnsureAppUser(ctx, owner.ID, "end-user-b", "", key.ID)
	if err != nil {
		t.Fatalf("EnsureAppUser(B): %v", err)
	}

	for _, rec := range []*store.AgentRecord{
		{ID: "agt_site", UserID: owner.ID, Name: "Site Agent"},
		{ID: "agt_user_a", UserID: appUserA.ID, Name: "User A Agent"},
		{ID: "agt_user_b", UserID: appUserB.ID, Name: "User B Agent"},
	} {
		if err := st.SaveAgent(ctx, rec); err != nil {
			t.Fatalf("SaveAgent(%s): %v", rec.ID, err)
		}
	}

	resolver, err := NewResolver(st)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	ident, err := resolver.ResolveBearer(ctx, token)
	if err != nil {
		t.Fatalf("ResolveBearer: %v", err)
	}
	switched, err := resolver.SwitchToAppUser(ctx, ident, "end-user-a")
	if err != nil {
		t.Fatalf("SwitchToAppUser: %v", err)
	}

	if switched.EffectiveUserID() != appUserA.ID {
		t.Fatalf("EffectiveUserID = %q, want %q", switched.EffectiveUserID(), appUserA.ID)
	}
	if !switched.CanAccessAgent("agt_site") {
		t.Fatal("switched identity lost access to the site owner's Agent")
	}
	if !switched.CanAccessAgent("agt_user_a") {
		t.Fatal("switched identity cannot access its own app_user Agent")
	}
	if switched.CanAccessAgent("agt_user_b") {
		t.Fatal("switched identity can access another app_user's Agent")
	}
}
