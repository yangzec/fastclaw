package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/store"
)

type wecomCreateScheduleArgs struct {
	Summary     string `json:"summary"`
	Start       string `json:"start"`
	End         string `json:"end"`
	Description string `json:"description"`
	Location    string `json:"location"`
	Attendees   string `json:"attendees"`
	WholeDay    bool   `json:"whole_day"`
	RemindSecs  int    `json:"remind_before_secs"`
}

type wecomGetScheduleArgs struct {
	ScheduleID string `json:"schedule_id"`
}

func lookupWeComChannel(ctx context.Context, st store.Store, r *Registry, agentID string) (*store.ChannelRecord, error) {
	if st == nil {
		return nil, fmt.Errorf("wecom schedule: store unavailable")
	}
	prefer := ""
	if r != nil && r.MessageChannel() == "wecom" {
		prefer = r.MessageAccountID()
	}
	var owners []string
	if r != nil && r.OwnerUserID() != "" {
		owners = append(owners, r.OwnerUserID())
	}
	owners = append(owners, "")
	for _, owner := range owners {
		rows, err := st.ListChannels(ctx, owner, agentID)
		if err != nil || len(rows) == 0 {
			continue
		}
		if prefer != "" {
			for i := range rows {
				if rows[i].Type == "wecom" && rows[i].AccountID == prefer {
					return &rows[i], nil
				}
			}
		}
		for i := range rows {
			if rows[i].Type == "wecom" {
				return &rows[i], nil
			}
		}
	}
	return nil, fmt.Errorf("no WeCom bot is connected to this agent")
}

func wecomSenderUserID(ctx context.Context, st store.Store, r *Registry) string {
	if r == nil || r.MessageChannel() != "wecom" {
		return ""
	}
	if chatID := strings.TrimSpace(r.MessageChatID()); chatID != "" && !strings.HasPrefix(chatID, "wr") {
		return chatID
	}
	if st == nil {
		return ""
	}
	uid := r.ChatterUserID()
	if uid == "" {
		return ""
	}
	u, err := st.GetUser(ctx, uid)
	if err != nil || u == nil {
		return ""
	}
	rest, ok := strings.CutPrefix(u.ExternalID, "wecom:")
	if !ok {
		return ""
	}
	if i := strings.LastIndex(rest, ":"); i >= 0 {
		return rest[i+1:]
	}
	return rest
}

func splitWeComUserIDs(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\n'
	})
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

func parseWeComWhen(raw string, loc *time.Location) (unix int64, dateOnly bool, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false, fmt.Errorf("empty time")
	}
	if n, nerr := strconv.ParseInt(raw, 10, 64); nerr == nil && n > 1_000_000_000 {
		return n, false, nil
	}
	if loc == nil {
		loc = time.Local
	}
	if t, perr := time.Parse(time.RFC3339, raw); perr == nil {
		return t.Unix(), false, nil
	}
	for _, f := range []string{
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
	} {
		if t, perr := time.ParseInLocation(f, raw, loc); perr == nil {
			return t.Unix(), false, nil
		}
	}
	if t, perr := time.ParseInLocation("2006-01-02", raw, loc); perr == nil {
		return t.Unix(), true, nil
	}
	return 0, false, fmt.Errorf("unrecognized time %q (use ISO-8601, 'YYYY-MM-DD HH:MM', or unix seconds)", raw)
}
