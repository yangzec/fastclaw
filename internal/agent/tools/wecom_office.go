package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/channels"
	"github.com/fastclaw-ai/fastclaw/internal/scope"
	"github.com/fastclaw-ai/fastclaw/internal/store"
)

type wecomListSchedulesArgs struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type wecomSearchSchedulesArgs struct {
	Keywords string `json:"keywords"`
	Start    string `json:"start"`
	End      string `json:"end"`
}

type wecomCancelScheduleArgs struct {
	ScheduleID   string `json:"schedule_id"`
	ConfirmToken string `json:"confirm_token,omitempty"`
}

type wecomCreateDocArgs struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

type wecomReadDocArgs struct {
	DocumentID string `json:"document_id"`
}

type wecomAppendDocArgs struct {
	DocumentID   string `json:"document_id"`
	Content      string `json:"content"`
	ConfirmToken string `json:"confirm_token,omitempty"`
}

type wecomSearchDocsArgs struct {
	Keywords string `json:"keywords"`
}

type wecomLookupContactArgs struct {
	Keywords string `json:"keywords"`
}

// wecomCLIFromChannel is swapped in tests so CLI calls hit httptest.
var wecomCLIFromChannel = channels.WeComCLIFromChannel

// RegisterWeComOfficeTools exposes 企业微信智能机器人 CLI calendar / docs
// (same BotID + Secret as the IM long-conn). 可使用权限 must already be
// granted on the bot; this does not use 自建应用 CorpID.
func RegisterWeComOfficeTools(r *Registry, st store.Store, agentID string) {
	r.Register("wecom_create_schedule",
		"Create an official 企业微信 calendar event via the connected intelligent robot (appears in WeCom 日程, can invite colleagues). Use this when the user wants something written into the WeCom calendar — a meeting, invite, or calendar block. Do NOT use create_cron_job for that: cron only pings this agent later; it does not create a WeCom schedule. start/end are the chatter's local time unless they include an offset. attendees are WeCom userids (comma-separated). When chatting on WeCom, the current sender is invited automatically if attendees is empty.",
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
		"Fetch official 企业微信 schedule details by schedule_id returned from wecom_create_schedule / list / search.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"schedule_id": map[string]interface{}{
					"type":        "string",
					"description": "The schedule_id from create/list/search.",
				},
			},
			"required": []string{"schedule_id"},
		},
		makeWeComGetSchedule(st, r, agentID),
	)

	r.Register("wecom_list_schedules",
		"List official 企业微信 calendar events in a time range (wall clock in the chatter's timezone). If start/end are omitted, lists today through the next 7 days. Query window is roughly ±30 days from now.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"start": map[string]interface{}{
					"type":        "string",
					"description": "Range start (same formats as wecom_create_schedule start).",
				},
				"end": map[string]interface{}{
					"type":        "string",
					"description": "Range end. Must be sent together with start.",
				},
			},
		},
		makeWeComListSchedules(st, r, agentID),
	)

	r.Register("wecom_search_schedules",
		"Search official 企业微信 calendar events by subject keyword. Use this when the user names a meeting; use wecom_list_schedules when they only give a date.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"keywords": map[string]interface{}{
					"type":        "string",
					"description": "Search keywords, comma or space separated.",
				},
				"start": map[string]interface{}{
					"type":        "string",
					"description": "Optional range start.",
				},
				"end": map[string]interface{}{
					"type":        "string",
					"description": "Optional range end.",
				},
			},
			"required": []string{"keywords"},
		},
		makeWeComSearchSchedules(st, r, agentID),
	)

	r.Register("wecom_cancel_schedule",
		"Cancel an official 企业微信 calendar event. REQUIRES two-step confirmation: first call returns a preview + confirm_token. Show that to the user. Only call again with confirm_token after they explicitly agree. Never invent a token.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"schedule_id": map[string]interface{}{
					"type":        "string",
					"description": "schedule_id from list/search/create.",
				},
				"confirm_token": map[string]interface{}{
					"type":        "string",
					"description": "Token from the first call. Required to apply. Omit on the preview call.",
				},
			},
			"required": []string{"schedule_id"},
		},
		makeWeComCancelSchedule(st, r, agentID),
	)

	r.Register("wecom_create_doc",
		"Create an official 企业微信 online document (doc) via the connected intelligent robot and share it with the current WeCom sender so they can open it. Use this when the user wants a real 企微文档, not a workspace file.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"title": map[string]interface{}{
					"type":        "string",
					"description": "Document title.",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "Optional initial body (plain text or markdown).",
				},
			},
			"required": []string{"title"},
		},
		makeWeComCreateDoc(st, r, agentID),
	)

	r.Register("wecom_read_doc",
		"Read an official 企业微信 document by docid or a https://doc.weixin.qq.com/doc/… URL. The robot must have access (it owns docs it created, or was added as a collaborator).",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"document_id": map[string]interface{}{
					"type":        "string",
					"description": "docid or a WeCom /doc/ URL.",
				},
			},
			"required": []string{"document_id"},
		},
		makeWeComReadDoc(st, r, agentID),
	)

	r.Register("wecom_append_doc",
		"Append plain text to the end of an official 企业微信 document. REQUIRES two-step confirmation: first call returns a preview + confirm_token. Show that to the user. Only call again with confirm_token after they explicitly agree. Never invent a token. Does not replace the existing body.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"document_id": map[string]interface{}{
					"type":        "string",
					"description": "docid or a /doc/ URL.",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "Text to append.",
				},
				"confirm_token": map[string]interface{}{
					"type":        "string",
					"description": "Token from the first call. Required to apply. Omit on the preview call.",
				},
			},
			"required": []string{"document_id", "content"},
		},
		makeWeComAppendDoc(st, r, agentID),
	)

	r.Register("wecom_search_docs",
		"Search official 企业微信 documents by keyword (title and content). Returns docid, name, and URL.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"keywords": map[string]interface{}{
					"type":        "string",
					"description": "Search keywords, comma or space separated.",
				},
			},
			"required": []string{"keywords"},
		},
		makeWeComSearchDocs(st, r, agentID),
	)

	r.Register("wecom_lookup_contact",
		"Search 企业微信 contacts by name / pinyin / alias and return userids. Use this before inviting people to a schedule when the user gave names instead of userids.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"keywords": map[string]interface{}{
					"type":        "string",
					"description": "Names to search, comma or space separated (max 10).",
				},
			},
			"required": []string{"keywords"},
		},
		makeWeComLookupContact(st, r, agentID),
	)
}

// RegisterWeComScheduleTools is the historical name; office tools include
// the original schedule pair plus list/search/docs.
func RegisterWeComScheduleTools(r *Registry, st store.Store, agentID string) {
	RegisterWeComOfficeTools(r, st, agentID)
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
		client, err := wecomCLIClient(ctx, st, r, agentID)
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
			endUnix = startUnix + 24*60*60 - 1
		} else {
			endUnix = startUnix + 60*60
		}
		begin := formatWeComCLITime(startUnix, loc)
		end := formatWeComCLITime(endUnix, loc)
		if wholeDay {
			day := time.Unix(startUnix, 0).In(loc)
			begin = day.Format("2006-01-02") + " 00:00:00"
			end = day.Format("2006-01-02") + " 23:59:59"
		}
		atts := splitWeComUserIDs(args.Attendees)
		if len(atts) == 0 {
			if me := wecomSenderUserID(ctx, st, r); me != "" {
				atts = []string{me}
			}
		}
		id, _, err := client.CreateSchedule(ctx, channels.WeComCLISchedule{
			Subject:     args.Summary,
			BeginTime:   begin,
			EndTime:     end,
			Description: args.Description,
			Location:    args.Location,
			Attendees:   atts,
			WholeDay:    wholeDay,
			RemindSecs:  args.RemindSecs,
		})
		if err != nil {
			return "", err
		}
		msg := fmt.Sprintf("Created WeCom schedule %s (%s).", id, strings.TrimSpace(args.Summary))
		if id == "" {
			msg = fmt.Sprintf("Created WeCom schedule (%s).", strings.TrimSpace(args.Summary))
		}
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
		client, err := wecomCLIClient(ctx, st, r, agentID)
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

func makeWeComListSchedules(st store.Store, r *Registry, agentID string) ToolFunc {
	return func(ctx context.Context, rawArgs json.RawMessage) (string, error) {
		var args wecomListSchedulesArgs
		_ = json.Unmarshal(rawArgs, &args)
		client, err := wecomCLIClient(ctx, st, r, agentID)
		if err != nil {
			return "", err
		}
		tzName := scope.Timezone(ctx, st, r.ChatterUserID(), agentID)
		loc := scope.LoadLocationOrLocal(tzName)
		begin, end, err := wecomCLIRange(args.Start, args.End, loc, true)
		if err != nil {
			return "", err
		}
		raw, err := client.ListSchedules(ctx, begin, end)
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
}

func makeWeComSearchSchedules(st store.Store, r *Registry, agentID string) ToolFunc {
	return func(ctx context.Context, rawArgs json.RawMessage) (string, error) {
		var args wecomSearchSchedulesArgs
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
		kws := splitWeComUserIDs(args.Keywords)
		if len(kws) == 0 {
			return "", fmt.Errorf("keywords required")
		}
		client, err := wecomCLIClient(ctx, st, r, agentID)
		if err != nil {
			return "", err
		}
		tzName := scope.Timezone(ctx, st, r.ChatterUserID(), agentID)
		loc := scope.LoadLocationOrLocal(tzName)
		begin, end, err := wecomCLIRange(args.Start, args.End, loc, false)
		if err != nil {
			return "", err
		}
		raw, err := client.SearchSchedules(ctx, kws, begin, end)
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
}

func makeWeComCancelSchedule(st store.Store, r *Registry, agentID string) ToolFunc {
	return func(ctx context.Context, rawArgs json.RawMessage) (string, error) {
		var args wecomCancelScheduleArgs
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
		id := strings.TrimSpace(args.ScheduleID)
		if id == "" {
			return "", fmt.Errorf("schedule_id required")
		}
		if strings.TrimSpace(args.ConfirmToken) != "" {
			p, err := wecomTakePending(agentID, args.ConfirmToken)
			if err != nil {
				return "", err
			}
			if p.Kind != "cancel_schedule" {
				return "", fmt.Errorf("confirm_token is not for cancelling a schedule")
			}
			id = p.SchedID
			client, err := wecomCLIClient(ctx, st, r, agentID)
			if err != nil {
				return "", err
			}
			if err := client.CancelSchedule(ctx, id); err != nil {
				return "", err
			}
			return "Cancelled WeCom schedule " + id + ".", nil
		}
		tok := wecomStorePending(wecomPending{
			AgentID: agentID,
			Kind:    "cancel_schedule",
			Preview: "Cancel WeCom schedule " + id + ".",
			SchedID: id,
		})
		return wecomConfirmPrompt("Cancel WeCom schedule "+id+".", tok), nil
	}
}

func makeWeComCreateDoc(st store.Store, r *Registry, agentID string) ToolFunc {
	return func(ctx context.Context, rawArgs json.RawMessage) (string, error) {
		var args wecomCreateDocArgs
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
		title := strings.TrimSpace(args.Title)
		if title == "" {
			return "", fmt.Errorf("title required")
		}
		client, err := wecomCLIClient(ctx, st, r, agentID)
		if err != nil {
			return "", err
		}
		raw, err := client.CreateDoc(ctx, title, args.Content)
		if err != nil {
			return "", err
		}
		id, url, name := channelsPickDoc(raw)
		if me := wecomSenderUserID(ctx, st, r); me != "" && id != "" {
			_ = client.ShareDoc(ctx, id, me)
		}
		if name == "" {
			name = title
		}
		if url != "" {
			return fmt.Sprintf("Created WeCom doc [%s](%s).", name, url), nil
		}
		if id != "" {
			return fmt.Sprintf("Created WeCom doc %s (%s).", name, id), nil
		}
		return "Created WeCom doc " + name + ".", nil
	}
}

func makeWeComReadDoc(st store.Store, r *Registry, agentID string) ToolFunc {
	return func(ctx context.Context, rawArgs json.RawMessage) (string, error) {
		var args wecomReadDocArgs
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
		id := channels.WeComCLIDocID(args.DocumentID)
		if id == "" {
			return "", fmt.Errorf("document_id required")
		}
		client, err := wecomCLIClient(ctx, st, r, agentID)
		if err != nil {
			return "", err
		}
		raw, err := client.GetDoc(ctx, id)
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
}

func makeWeComAppendDoc(st store.Store, r *Registry, agentID string) ToolFunc {
	return func(ctx context.Context, rawArgs json.RawMessage) (string, error) {
		var args wecomAppendDocArgs
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
		id := channels.WeComCLIDocID(args.DocumentID)
		content := strings.TrimSpace(args.Content)
		if id == "" || content == "" {
			return "", fmt.Errorf("document_id and content required")
		}
		if strings.TrimSpace(args.ConfirmToken) != "" {
			p, err := wecomTakePending(agentID, args.ConfirmToken)
			if err != nil {
				return "", err
			}
			if p.Kind != "append_doc" {
				return "", fmt.Errorf("confirm_token is not for modifying a document")
			}
			id, content = p.DocID, p.DocAppend
			client, err := wecomCLIClient(ctx, st, r, agentID)
			if err != nil {
				return "", err
			}
			if err := client.AppendDoc(ctx, id, content); err != nil {
				return "", err
			}
			return "Appended to WeCom doc " + id + ".", nil
		}
		preview := fmt.Sprintf("Append to WeCom doc %s:\n%s", id, content)
		tok := wecomStorePending(wecomPending{
			AgentID:   agentID,
			Kind:      "append_doc",
			Preview:   preview,
			DocID:     id,
			DocAppend: content,
		})
		return wecomConfirmPrompt(preview, tok), nil
	}
}

func makeWeComSearchDocs(st store.Store, r *Registry, agentID string) ToolFunc {
	return func(ctx context.Context, rawArgs json.RawMessage) (string, error) {
		var args wecomSearchDocsArgs
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
		kws := splitWeComUserIDs(args.Keywords)
		if len(kws) == 0 {
			return "", fmt.Errorf("keywords required")
		}
		client, err := wecomCLIClient(ctx, st, r, agentID)
		if err != nil {
			return "", err
		}
		raw, err := client.SearchDocs(ctx, kws)
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
}

func makeWeComLookupContact(st store.Store, r *Registry, agentID string) ToolFunc {
	return func(ctx context.Context, rawArgs json.RawMessage) (string, error) {
		var args wecomLookupContactArgs
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
		kws := splitWeComUserIDs(args.Keywords)
		if len(kws) == 0 {
			return "", fmt.Errorf("keywords required")
		}
		if len(kws) > 10 {
			kws = kws[:10]
		}
		client, err := wecomCLIClient(ctx, st, r, agentID)
		if err != nil {
			return "", err
		}
		raw, err := client.SearchUsers(ctx, kws)
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
}

func wecomCLIClient(ctx context.Context, st store.Store, r *Registry, agentID string) (*channels.WeComCLI, error) {
	ch, err := lookupWeComChannel(ctx, st, r, agentID)
	if err != nil {
		return nil, err
	}
	return wecomCLIFromChannel(ch)
}

func wecomCLIRange(start, end string, loc *time.Location, defaultWeek bool) (begin, finish string, err error) {
	start, end = strings.TrimSpace(start), strings.TrimSpace(end)
	if start == "" && end == "" {
		if !defaultWeek {
			return "", "", nil
		}
		now := time.Now().In(loc)
		day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		return day.Format("2006-01-02 15:04:05"), day.AddDate(0, 0, 7).Add(24*time.Hour - time.Second).Format("2006-01-02 15:04:05"), nil
	}
	if start == "" || end == "" {
		return "", "", fmt.Errorf("start and end must be sent together")
	}
	su, _, err := parseWeComWhen(start, loc)
	if err != nil {
		return "", "", fmt.Errorf("start: %w", err)
	}
	eu, _, err := parseWeComWhen(end, loc)
	if err != nil {
		return "", "", fmt.Errorf("end: %w", err)
	}
	return formatWeComCLITime(su, loc), formatWeComCLITime(eu, loc), nil
}

func formatWeComCLITime(unix int64, loc *time.Location) string {
	if loc == nil {
		loc = time.Local
	}
	return time.Unix(unix, 0).In(loc).Format("2006-01-02 15:04:05")
}

func channelsPickDoc(raw json.RawMessage) (id, url, name string) {
	return channels.WeComCLIPickDoc(raw)
}
