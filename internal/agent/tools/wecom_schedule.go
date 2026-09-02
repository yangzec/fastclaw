package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/channels"
	"github.com/fastclaw-ai/fastclaw/internal/scope"
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

// wecomOAFromChannel is swapped in tests so schedule/add hits httptest.
var wecomOAFromChannel = channels.WeComOAFromChannel

// RegisterWeComScheduleTools exposes official 企业微信日程 APIs as
// IM-native tools. Credentials come from the WeCom channel's 自建应用
// (CorpID + Secret), not the AI-bot long-conn secret.
func RegisterWeComScheduleTools(r *Registry, st store.Store, agentID string) {
	r.Register("wecom_create_schedule",
		"Create an official 企业微信 calendar event (appears in WeCom 日程, can invite colleagues). On WeCom this is the default for 日程 / 开会 / 占日历. Do NOT use create_cron_job for that: cron only pings this agent later; it does not create a WeCom schedule. start/end are the chatter's local time unless they include an offset. attendees are WeCom userids (comma-separated). When chatting on WeCom, the current sender is invited automatically if attendees is empty.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"summary": map[string]interface{}{
					"type":        "string",
					"description": "Event title, e.g. 项目例会.",
				},
				"start": map[string]interface{}{
					"type":        "string",
					"description": "Start as ISO-8601 (2026-09-02T15:00:00), 'YYYY-MM-DD HH:MM', a date-only day, or unix seconds.",
				},
				"end": map[string]interface{}{
					"type":        "string",
					"description": "End in the same formats as start. Defaults to start+1h (or +1 day for all-day events).",
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "Optional event body.",
				},
				"location": map[string]interface{}{
					"type":        "string",
					"description": "Optional location / meeting room name.",
				},
				"attendees": map[string]interface{}{
					"type":        "string",
					"description": "Comma-separated WeCom userids to invite. Empty = current WeCom sender when the turn is on wecom.",
				},
				"whole_day": map[string]interface{}{
					"type":        "boolean",
					"description": "All-day event. Also implied when start is a date without a time.",
				},
				"remind_before_secs": map[string]interface{}{
					"type":        "integer",
					"description": "WeCom popup reminder seconds before start (e.g. 900 = 15 minutes). 0 = none.",
				},
			},
			"required": []string{"summary", "start"},
		},
		makeWeComCreateSchedule(st, r, agentID),
	)

	r.Register("wecom_get_schedule",
		"Fetch an official 企业微信 schedule by schedule_id returned from wecom_create_schedule.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"schedule_id": map[string]interface{}{
					"type":        "string",
					"description": "The schedule_id from wecom_create_schedule.",
				},
			},
			"required": []string{"schedule_id"},
		},
		makeWeComGetSchedule(st, r, agentID),
	)
}

func makeWeComCreateSchedule(st store.Store, r *Registry, agentID string) ToolFunc {
	return func(ctx context.Context, rawArgs json.RawMessage) (string, error) {
		var args wecomCreateScheduleArgs
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
		if strings.TrimSpace(args.Summary) == "" || strings.TrimSpace(args.Start) == "" {
			return "", fmt.Errorf("summary and start are required")
		}
		client, err := wecomOAClient(ctx, st, r, agentID)
		if err != nil {
			return "", err
		}
		tzName := scope.Timezone(ctx, st, r.ChatterUserID(), agentID)
		loc := scope.LoadLocationOrLocal(tzName)
		startUnix, startDay, err := parseWeComWhen(args.Start, loc)
		if err != nil {
			return "", fmt.Errorf("start: %w", err)
		}
		wholeDay := args.WholeDay || (startDay && strings.TrimSpace(args.End) == "")
		endUnix := startUnix
		if strings.TrimSpace(args.End) != "" {
			endUnix, _, err = parseWeComWhen(args.End, loc)
			if err != nil {
				return "", fmt.Errorf("end: %w", err)
			}
		} else if wholeDay {
			endUnix = startUnix + 24*60*60
		} else {
			endUnix = startUnix + 60*60
		}
		atts := splitWeComUserIDs(args.Attendees)
		if len(atts) == 0 {
			if me := wecomSenderUserID(ctx, st, r); me != "" {
				atts = []string{me}
			}
		}
		id, err := client.AddSchedule(ctx, channels.WeComSchedule{
			Summary:     args.Summary,
			Description: args.Description,
			Location:    args.Location,
			StartUnix:   startUnix,
			EndUnix:     endUnix,
			WholeDay:    wholeDay,
			Attendees:   atts,
			RemindSecs:  args.RemindSecs,
		})
		if err != nil {
			return "", err
		}
		msg := fmt.Sprintf("Created WeCom schedule %s (%s).", id, strings.TrimSpace(args.Summary))
		if len(atts) > 0 {
			msg += " Invited: " + strings.Join(atts, ", ") + "."
		}
		return msg, nil
	}
}

func makeWeComGetSchedule(st store.Store, r *Registry, agentID string) ToolFunc {
	return func(ctx context.Context, rawArgs json.RawMessage) (string, error) {
		var args wecomGetScheduleArgs
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
		id := strings.TrimSpace(args.ScheduleID)
		if id == "" {
			return "", fmt.Errorf("schedule_id required")
		}
		client, err := wecomOAClient(ctx, st, r, agentID)
		if err != nil {
			return "", err
		}
		raw, err := client.GetSchedules(ctx, []string{id})
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
}

func wecomOAClient(ctx context.Context, st store.Store, r *Registry, agentID string) (*channels.WeComOA, error) {
	ch, err := lookupWeComChannel(ctx, st, r, agentID)
	if err != nil {
		return nil, err
	}
	return wecomOAFromChannel(ch)
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
