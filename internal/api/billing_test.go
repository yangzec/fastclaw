package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fastclaw-ai/fastclaw/internal/auth"
	"github.com/fastclaw-ai/fastclaw/internal/usage"
)

func billingServer(t *testing.T) (*Server, *usage.MemMeter, *usage.MemQuotaStore) {
	t.Helper()
	meter := usage.NewMemMeter()
	qs := usage.NewMemQuotaStore()
	s := NewServer(nil, nil, nil)
	s.SetMeter(meter)
	s.SetQuotaStore(qs)
	return s, meter, qs
}

func withIdent(req *http.Request, ident auth.Identity) *http.Request {
	return req.WithContext(auth.WithIdentity(req.Context(), ident))
}

func TestGetUsageAlwaysOwnerBucket(t *testing.T) {
	s, meter, _ := billingServer(t)
	ctx := context.Background()
	_ = meter.RecordTokens(ctx, "u_owner", "agt_support", "site:alice:1", "openai", "gpt", usage.Tokens{Input: 10, Output: 2})
	_ = meter.RecordTokens(ctx, "u_app", "agt_support", "site:alice:1", "openai", "gpt", usage.Tokens{Input: 99, Output: 9})

	req := httptest.NewRequest(http.MethodGet, "/v1/usage?user_id=u_app&days=7", nil)
	req = withIdent(req, auth.Identity{UserID: "u_app", OwnerUserID: "u_owner", AuthMethod: "apikey"})
	rr := httptest.NewRecorder()
	s.HandleGetUsage(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var body struct {
		UserID string       `json:"userId"`
		Totals usage.Totals `json:"totals"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.UserID != "u_owner" {
		t.Fatalf("userId = %q, want owner", body.UserID)
	}
	if body.Totals.Input != 10 || body.Totals.Output != 2 {
		t.Fatalf("totals = %+v, want owner tokens not app_user", body.Totals)
	}
}

func TestSetQuotaRejectsForeignUser(t *testing.T) {
	s, _, _ := billingServer(t)
	req := httptest.NewRequest(http.MethodPut, "/v1/quota", bytes.NewBufferString(`{"user_id":"u_other","monthly_token_limit":1}`))
	req = withIdent(req, auth.Identity{UserID: "u_owner", OwnerUserID: "u_owner", AuthMethod: "apikey"})
	rr := httptest.NewRecorder()
	s.HandleSetQuota(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403: %s", rr.Code, rr.Body.String())
	}
}

func TestSetQuotaDefaultsToOwner(t *testing.T) {
	s, _, qs := billingServer(t)
	req := httptest.NewRequest(http.MethodPut, "/v1/quota", bytes.NewBufferString(`{"monthly_token_limit":5000,"reset_day":1}`))
	req = withIdent(req, auth.Identity{UserID: "u_app", OwnerUserID: "u_owner", AuthMethod: "apikey"})
	rr := httptest.NewRecorder()
	s.HandleSetQuota(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	q, err := qs.GetQuota(context.Background(), "u_owner")
	if err != nil {
		t.Fatal(err)
	}
	if q.MonthlyTokenLimit != 5000 {
		t.Fatalf("quota %+v", q)
	}
}
