package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fastclaw-ai/fastclaw/internal/channels"
	"github.com/fastclaw-ai/fastclaw/internal/scope"
	"github.com/fastclaw-ai/fastclaw/internal/store"
)

type feishuCreateEventArgs struct {
	Summary     string `json:"summary"`
	Start       string `json:"start"`
	End         string `json:"end"`
	Description string `json:"description"`
	Location    string `json:"location"`
	Attendees   string `json:"attendees"`
	WholeDay    bool   `json:"whole_day"`
	RemindMins  int    `json:"remind_before_mins"`
}

type feishuCreateTaskArgs struct {
	Summary     string `json:"summary"`
	Description string `json:"description"`
	Due         string `json:"due"`
	Assignees   string `json:"assignees"`
	WholeDay    bool   `json:"whole_day"`
}

type feishuCompleteTaskArgs struct {
	TaskGUID string `json:"task_guid"`
}

type feishuCreateDocArgs struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

type feishuReadDocArgs struct {
	DocumentID string `json:"document_id"`
}

// feishuOpenAPIFromChannel is swapped in tests so OpenAPI calls hit httptest.
var feishuOpenAPIFromChannel = channels.FeishuOpenAPIFromChannel

// RegisterFeishuOfficeTools exposes official Feishu calendar / task /
// docs APIs as IM-native tools. Credentials come from the connected
// Feishu channel (QR bot or pasted App ID + Secret).
func RegisterFeishuOfficeTools(r *Registry, st store.Store, agentID string) {
	r.Register("feishu_create_event",
		"Create an official Feishu / Lark calendar event (appears in 飞书日程, can invite colleagues). Use this when the user wants something written into the Feishu calendar — a meeting, invite, or calendar block. Do NOT use create_cron_job for that: cron only pings this agent later; it does not create a Feishu event. start/end are the chatter's local time unless they include an offset. attendees are Feishu open_id values (comma-separated). When chatting on Feishu, the current sender is invited automatically if attendees is empty.",
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
					"description": "Comma-separated Feishu open_id values to invite. Empty = current Feishu sender when the turn is on feishu.",
				},
				"whole_day": map[string]interface{}{
					"type":        "boolean",
					"description": "All-day event. Also implied when start is a date without a time.",
				},
				"remind_before_mins": map[string]interface{}{
					"type":        "integer",
					"description": "Popup reminder minutes before start (e.g. 15). 0 = none.",
				},
			},
			"required": []string{"summary", "start"},
		},
		makeFeishuCreateEvent(st, r, agentID),
	)

	r.Register("feishu_create_task",
		"Create an official Feishu / Lark task (飞书待办 / 任务中心). Use this when the user wants a real Feishu todo assigned to someone. Do NOT use create_cron_job for a todo list item. assignees are Feishu open_id values. When chatting on Feishu, the current sender is assigned if assignees is empty.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"summary": map[string]interface{}{
					"type":        "string",
					"description": "Task title.",
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "Optional task notes.",
				},
				"due": map[string]interface{}{
					"type":        "string",
					"description": "Optional due time (same formats as feishu_create_event start).",
				},
				"assignees": map[string]interface{}{
					"type":        "string",
					"description": "Comma-separated Feishu open_id assignees. Empty = current Feishu sender.",
				},
				"whole_day": map[string]interface{}{
					"type":        "boolean",
					"description": "Due is a date without a time.",
				},
			},
			"required": []string{"summary"},
		},
		makeFeishuCreateTask(st, r, agentID),
	)

	r.Register("feishu_complete_task",
		"Mark an official Feishu task complete by the task_guid returned from feishu_create_task.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"task_guid": map[string]interface{}{
					"type":        "string",
					"description": "The guid from feishu_create_task.",
				},
			},
			"required": []string{"task_guid"},
		},
		makeFeishuCompleteTask(st, r, agentID),
	)

	r.Register("feishu_create_doc",
		"Create an official Feishu / Lark cloud document (新版文档 docx) and share it with the current Feishu sender so they can open it. Use this when the user wants a real 飞书文档, not a workspace file.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"title": map[string]interface{}{
					"type":        "string",
					"description": "Document title.",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "Optional plain-text body (one paragraph per line).",
				},
			},
			"required": []string{"title"},
		},
		makeFeishuCreateDoc(st, r, agentID),
	)

	r.Register("feishu_read_doc",
		"Read the title and plain text of an official Feishu / Lark document by document_id or a https://…/docx/… URL. The bot must have access (it owns docs it created, or was added as a collaborator).",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"document_id": map[string]interface{}{
					"type":        "string",
					"description": "document_id or a Feishu/Lark /docx/ URL.",
				},
			},
			"required": []string{"document_id"},
		},
		makeFeishuReadDoc(st, r, agentID),
	)
}

func makeFeishuCreateEvent(st store.Store, r *Registry, agentID string) ToolFunc {
	return func(ctx context.Context, rawArgs json.RawMessage) (string, error) {
		var args feishuCreateEventArgs
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
		if strings.TrimSpace(args.Summary) == "" || strings.TrimSpace(args.Start) == "" {
			return "", fmt.Errorf("summary and start are required")
		}
		client, err := feishuOfficeClient(ctx, st, r, agentID)
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
			if me := feishuSenderOpenID(ctx, st, r); me != "" {
				atts = []string{me}
			}
		}
		id, _, link, err := client.CreateEvent(ctx, channels.FeishuEvent{
			Summary:     args.Summary,
			Description: args.Description,
			Location:    args.Location,
			StartUnix:   startUnix,
			EndUnix:     endUnix,
			Timezone:    tzName,
			WholeDay:    wholeDay,
			Attendees:   atts,
			RemindMins:  args.RemindMins,
		})
		if err != nil {
			return "", err
		}
		msg := fmt.Sprintf("Created Feishu event %s (%s).", id, strings.TrimSpace(args.Summary))
		if link != "" {
			msg += " " + link
		}
		if len(atts) > 0 {
			msg += " Invited: " + strings.Join(atts, ", ") + "."
		}
		return msg, nil
	}
}

func makeFeishuCreateTask(st store.Store, r *Registry, agentID string) ToolFunc {
	return func(ctx context.Context, rawArgs json.RawMessage) (string, error) {
		var args feishuCreateTaskArgs
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
		if strings.TrimSpace(args.Summary) == "" {
			return "", fmt.Errorf("summary is required")
		}
		client, err := feishuOfficeClient(ctx, st, r, agentID)
		if err != nil {
			return "", err
		}
		tzName := scope.Timezone(ctx, st, r.ChatterUserID(), agentID)
		loc := scope.LoadLocationOrLocal(tzName)
		var dueUnix int64
		wholeDay := args.WholeDay
		if strings.TrimSpace(args.Due) != "" {
			var day bool
			dueUnix, day, err = parseWeComWhen(args.Due, loc)
			if err != nil {
				return "", fmt.Errorf("due: %w", err)
			}
			wholeDay = wholeDay || day
		}
		assignees := splitWeComUserIDs(args.Assignees)
		if len(assignees) == 0 {
			if me := feishuSenderOpenID(ctx, st, r); me != "" {
				assignees = []string{me}
			}
		}
		guid, link, err := client.CreateTask(ctx, channels.FeishuTask{
			Summary:     args.Summary,
			Description: args.Description,
			DueUnix:     dueUnix,
			WholeDay:    wholeDay,
			Assignees:   assignees,
		})
		if err != nil {
			return "", err
		}
		msg := fmt.Sprintf("Created Feishu task %s (%s).", guid, strings.TrimSpace(args.Summary))
		if link != "" {
			msg += " " + link
		}
		if len(assignees) > 0 {
			msg += " Assigned: " + strings.Join(assignees, ", ") + "."
		}
		return msg, nil
	}
}

func makeFeishuCompleteTask(st store.Store, r *Registry, agentID string) ToolFunc {
	return func(ctx context.Context, rawArgs json.RawMessage) (string, error) {
		var args feishuCompleteTaskArgs
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
		guid := strings.TrimSpace(args.TaskGUID)
		if guid == "" {
			return "", fmt.Errorf("task_guid required")
		}
		client, err := feishuOfficeClient(ctx, st, r, agentID)
		if err != nil {
			return "", err
		}
		if err := client.CompleteTask(ctx, guid); err != nil {
			return "", err
		}
		return "Completed Feishu task " + guid + ".", nil
	}
}

func makeFeishuCreateDoc(st store.Store, r *Registry, agentID string) ToolFunc {
	return func(ctx context.Context, rawArgs json.RawMessage) (string, error) {
		var args feishuCreateDocArgs
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
		if strings.TrimSpace(args.Title) == "" {
			return "", fmt.Errorf("title is required")
		}
		client, err := feishuOfficeClient(ctx, st, r, agentID)
		if err != nil {
			return "", err
		}
		var share []string
		if me := feishuSenderOpenID(ctx, st, r); me != "" {
			share = []string{me}
		}
		id, link, err := client.CreateDoc(ctx, channels.FeishuDoc{
			Title:   args.Title,
			Content: args.Content,
			Share:   share,
		})
		if err != nil {
			return "", err
		}
		msg := fmt.Sprintf("Created Feishu doc %s (%s).", id, strings.TrimSpace(args.Title))
		if link != "" {
			msg += " " + link
		}
		if len(share) > 0 {
			msg += " Shared with " + strings.Join(share, ", ") + "."
		}
		return msg, nil
	}
}

func makeFeishuReadDoc(st store.Store, r *Registry, agentID string) ToolFunc {
	return func(ctx context.Context, rawArgs json.RawMessage) (string, error) {
		var args feishuReadDocArgs
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
		ref := strings.TrimSpace(args.DocumentID)
		if ref == "" {
			return "", fmt.Errorf("document_id required")
		}
		client, err := feishuOfficeClient(ctx, st, r, agentID)
		if err != nil {
			return "", err
		}
		title, text, err := client.ReadDoc(ctx, ref)
		if err != nil {
			return "", err
		}
		if title == "" {
			title = "(untitled)"
		}
		if text == "" {
			return title, nil
		}
		return title + "\n\n" + text, nil
	}
}

func feishuOfficeClient(ctx context.Context, st store.Store, r *Registry, agentID string) (*channels.FeishuOpenAPI, error) {
	ch, err := lookupFeishuChannel(ctx, st, r, agentID)
	if err != nil {
		return nil, err
	}
	return feishuOpenAPIFromChannel(ch)
}

func lookupFeishuChannel(ctx context.Context, st store.Store, r *Registry, agentID string) (*store.ChannelRecord, error) {
	if st == nil {
		return nil, fmt.Errorf("feishu office: store unavailable")
	}
	prefer := ""
	if r != nil && r.MessageChannel() == "feishu" {
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
				if rows[i].Type == "feishu" && rows[i].AccountID == prefer {
					return &rows[i], nil
				}
			}
		}
		for i := range rows {
			if rows[i].Type == "feishu" {
				return &rows[i], nil
			}
		}
	}
	return nil, fmt.Errorf("no Feishu bot is connected to this agent — connect Feishu on the Channels page")
}

func feishuSenderOpenID(ctx context.Context, st store.Store, r *Registry) string {
	if r == nil || r.MessageChannel() != "feishu" {
		return ""
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
	rest, ok := strings.CutPrefix(u.ExternalID, "feishu:")
	if !ok {
		return ""
	}
	if i := strings.LastIndex(rest, ":"); i >= 0 {
		return rest[i+1:]
	}
	return rest
}
