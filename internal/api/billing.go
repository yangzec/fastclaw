package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/fastclaw-ai/fastclaw/internal/auth"
	"github.com/fastclaw-ai/fastclaw/internal/usage"
)

// HandleGetUsage handles GET /v1/usage.
//
// Returns per-day, per-agent token consumption for the API-key owner
// (the website FastClaw account). `user_id` and X-Fastclaw-End-User do
// not change the bucket — tokens are recorded on the agent owner, and
// upstream apps roll up by `daily[].agentId` or by their own session
// keys. Query `days` is the lookback window (default 30, max 90).
func (s *Server) HandleGetUsage(w http.ResponseWriter, r *http.Request) {
	if s.meter == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": map[string]string{"message": "usage metering not configured", "type": "server_error"},
		})
		return
	}

	ident, ok := auth.FromContext(r.Context())
	if !ok {
		writeUnauth(w, "authentication required")
		return
	}

	targetUser := ident.BillingUserID()
	if targetUser == "" {
		writeUnauth(w, "authentication required")
		return
	}

	days := 30
	if d := r.URL.Query().Get("days"); d != "" {
		if n, err := strconv.Atoi(d); err == nil && n > 0 && n <= 90 {
			days = n
		}
	}

	rang := usage.LastN(days)

	daily, err := s.meter.DailyForUser(r.Context(), targetUser, rang)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]string{"message": err.Error(), "type": "server_error"},
		})
		return
	}

	totals, err := s.meter.TotalsForUser(r.Context(), targetUser, rang)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]string{"message": err.Error(), "type": "server_error"},
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"userId": targetUser,
		"days":   days,
		"daily":  daily,
		"totals": totals,
	})
}

func billingOwner(r *http.Request) (auth.Identity, string, bool) {
	ident, ok := auth.FromContext(r.Context())
	if !ok {
		return ident, "", false
	}
	owner := ident.BillingUserID()
	return ident, owner, owner != ""
}

func rejectForeignQuotaUser(w http.ResponseWriter, owner, requested string) bool {
	if requested == "" || requested == owner {
		return false
	}
	writeJSON(w, http.StatusForbidden, map[string]any{
		"error": map[string]string{
			"message": "quota applies to the API-key owner account only",
			"type":    "authorization_error",
		},
	})
	return true
}

// HandleSetQuota handles PUT /v1/quota.
//
// Sets the monthly token/request ceiling for the API-key owner. The
// agent loop checks this before every LLM call. `user_id` is optional
// and must match the owner when set — quotas are a site-wide kill
// switch, not a per-app-user entitlement.
//
// Request body:
//
//	{
//	  "user_id": "u_xxx", // optional; must be the owner when set
//	  "monthly_token_limit": 5000000,
//	  "monthly_request_limit": 10000,
//	  "reset_day": 1
//	}
func (s *Server) HandleSetQuota(w http.ResponseWriter, r *http.Request) {
	if s.quotaStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": map[string]string{"message": "quota management not configured", "type": "server_error"},
		})
		return
	}

	_, owner, ok := billingOwner(r)
	if !ok {
		writeUnauth(w, "authentication required")
		return
	}

	var req struct {
		UserID              string `json:"user_id"`
		MonthlyTokenLimit   int64  `json:"monthly_token_limit"`
		MonthlyRequestLimit int64  `json:"monthly_request_limit"`
		ResetDay            int    `json:"reset_day"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"message": "invalid request body", "type": "invalid_request_error"},
		})
		return
	}
	if rejectForeignQuotaUser(w, owner, req.UserID) {
		return
	}
	if req.ResetDay < 1 || req.ResetDay > 28 {
		req.ResetDay = 1
	}

	q := &usage.Quota{
		UserID:              owner,
		MonthlyTokenLimit:   req.MonthlyTokenLimit,
		MonthlyRequestLimit: req.MonthlyRequestLimit,
		ResetDay:            req.ResetDay,
	}
	if err := s.quotaStore.SetQuota(r.Context(), q); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]string{"message": err.Error(), "type": "server_error"},
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"quota": q,
	})
}

// HandleGetQuota handles GET /v1/quota.
//
// Returns the current quota for the API-key owner. Query `user_id` is
// optional and must match the owner when set.
func (s *Server) HandleGetQuota(w http.ResponseWriter, r *http.Request) {
	if s.quotaStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": map[string]string{"message": "quota management not configured", "type": "server_error"},
		})
		return
	}

	_, owner, ok := billingOwner(r)
	if !ok {
		writeUnauth(w, "authentication required")
		return
	}
	if rejectForeignQuotaUser(w, owner, r.URL.Query().Get("user_id")) {
		return
	}

	q, err := s.quotaStore.GetQuota(r.Context(), owner)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": map[string]string{"message": "no quota configured for this user", "type": "not_found_error"},
		})
		return
	}

	// Also return current usage status.
	if s.meter != nil {
		status, err := usage.CheckQuota(r.Context(), s.quotaStore, s.meter, owner)
		if err == nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"quota":  q,
				"status": status,
			})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"quota": q,
	})
}

// HandleDeleteQuota handles DELETE /v1/quota.
//
// Removes the quota for the API-key owner (reverts to unlimited).
// Query `user_id` is optional and must match the owner when set.
func (s *Server) HandleDeleteQuota(w http.ResponseWriter, r *http.Request) {
	if s.quotaStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": map[string]string{"message": "quota management not configured", "type": "server_error"},
		})
		return
	}

	_, owner, ok := billingOwner(r)
	if !ok {
		writeUnauth(w, "authentication required")
		return
	}
	if rejectForeignQuotaUser(w, owner, r.URL.Query().Get("user_id")) {
		return
	}

	if err := s.quotaStore.DeleteQuota(r.Context(), owner); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]string{"message": err.Error(), "type": "server_error"},
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
