package auth

import "testing"

func TestBillingUserIDPrefersOwner(t *testing.T) {
	id := Identity{UserID: "u_app", OwnerUserID: "u_owner"}
	if got := id.BillingUserID(); got != "u_owner" {
		t.Fatalf("BillingUserID = %q, want owner", got)
	}
}

func TestBillingUserIDFallsBackToCaller(t *testing.T) {
	id := Identity{UserID: "u_session"}
	if got := id.BillingUserID(); got != "u_session" {
		t.Fatalf("BillingUserID = %q, want caller", got)
	}
}
