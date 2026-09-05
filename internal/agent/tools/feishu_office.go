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

type feishuCreateDocArgs struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

type feishuReadDocArgs struct {
	DocumentID string `json:"document_id"`
}

type feishuListTasksArgs struct {
	Completed string `json:"completed,omitempty"` // "", "true", "false"
}

type feishuGetTaskArgs struct {
	TaskGUID string `json:"task_guid"`
}

type feishuUpdateTaskArgs struct {
	TaskGUID     string `json:"task_guid"`
	Summary      string `json:"summary,omitempty"`
	Description  string `json:"description,omitempty"`
	Due          string `json:"due,omitempty"`
	ClearDue     bool   `json:"clear_due,omitempty"`
	WholeDay     bool   `json:"whole_day,omitempty"`
	Complete     *bool  `json:"complete,omitempty"`
	ConfirmToken string `json:"confirm_token,omitempty"`
}

type feishuCompleteTaskArgs struct {
	TaskGUID     string `json:"task_guid"`
	ConfirmToken string `json:"confirm_token,omitempty"`
}

type feishuAppendDocArgs struct {
	DocumentID   string `json:"document_id"`
	Content      string `json:"content"`
	Title        string `json:"title,omitempty"`
	ConfirmToken string `json:"confirm_token,omitempty"`
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

	r.Register("feishu_list_tasks",
		"List official Feishu / Lark tasks the connected bot can see (type=my_tasks). Use this to read 待办. completed empty = all, true = done only, false = open only. Personal todos the user created in the Feishu app may not appear — use feishu_get_task with a guid or todo link.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"completed": map[string]interface{}{
					"type":        "string",
					"description": "Filter: empty for all, \"true\" for done, \"false\" for open.",
				},
			},
		},
		makeFeishuListTasks(st, r, agentID),
	)

	r.Register("feishu_get_task",
		"Read one official Feishu / Lark task by task_guid or a Feishu todo URL that contains guid=.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"task_guid": map[string]interface{}{
					"type":        "string",
					"description": "Task guid from feishu_create_task / feishu_list_tasks, or a todo applink.",
				},
			},
			"required": []string{"task_guid"},
		},
		makeFeishuGetTask(st, r, agentID),
	)

	r.Register("feishu_complete_task",
		"Mark an official Feishu task complete. REQUIRES two-step confirmation: first call returns a preview + confirm_token. Show that to the user. Only call again with confirm_token after they explicitly agree. Never invent a token.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"task_guid": map[string]interface{}{
					"type":        "string",
					"description": "The guid from feishu_create_task or feishu_list_tasks.",
				},
				"confirm_token": map[string]interface{}{
					"type":        "string",
					"description": "Token from the first call. Required to actually complete. Omit on the preview call.",
				},
			},
			"required": []string{"task_guid"},
		},
		makeFeishuCompleteTask(st, r, agentID),
	)

	r.Register("feishu_update_task",
		"Change an official Feishu task title, notes, due time, or completion. REQUIRES two-step confirmation: first call previews the change and returns confirm_token. Ask the user. Only retry with that token after they agree. Never invent a token.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"task_guid": map[string]interface{}{
					"type":        "string",
					"description": "Task guid or todo URL.",
				},
				"summary": map[string]interface{}{
					"type":        "string",
					"description": "New title.",
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "New notes.",
				},
				"due": map[string]interface{}{
					"type":        "string",
					"description": "New due time (same formats as feishu_create_task).",
				},
				"clear_due": map[string]interface{}{
					"type":        "boolean",
					"description": "Remove the due time.",
				},
				"whole_day": map[string]interface{}{
					"type":        "boolean",
					"description": "Due is a date without a time.",
				},
				"complete": map[string]interface{}{
					"type":        "boolean",
					"description": "true = mark done, false = reopen.",
				},
				"confirm_token": map[string]interface{}{
					"type":        "string",
					"description": "Token from the first call. Required to apply. Omit on the preview call.",
				},
			},
			"required": []string{"task_guid"},
		},
		makeFeishuUpdateTask(st, r, agentID),
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

	r.Register("feishu_append_doc",
		"Modify an official Feishu / Lark document: append plain-text paragraphs and/or change the title. REQUIRES two-step confirmation: first call returns a preview + confirm_token. Show that to the user. Only call again with confirm_token after they explicitly agree. Never invent a token. Does not replace the existing body.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"document_id": map[string]interface{}{
					"type":        "string",
					"description": "document_id or a /docx/ URL.",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "Plain text to append (one paragraph per line). Optional if only changing the title.",
				},
				"title": map[string]interface{}{
					"type":        "string",
					"description": "Optional new document title.",
				},
				"confirm_token": map[string]interface{}{
					"type":        "string",
					"description": "Token from the first call. Required to apply. Omit on the preview call.",
				},
			},
			"required": []string{"document_id"},
		},
		makeFeishuAppendDoc(st, r, agentID),
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

func makeFeishuListTasks(st store.Store, r *Registry, agentID string) ToolFunc {
	return func(ctx context.Context, rawArgs json.RawMessage) (string, error) {
		var args feishuListTasksArgs
		if len(rawArgs) > 0 {
			if err := json.Unmarshal(rawArgs, &args); err != nil {
				return "", fmt.Errorf("parse args: %w", err)
			}
		}
		client, err := feishuOfficeClient(ctx, st, r, agentID)
		if err != nil {
			return "", err
		}
		var completed *bool
		switch strings.ToLower(strings.TrimSpace(args.Completed)) {
		case "true", "1", "done", "yes":
			v := true
			completed = &v
		case "false", "0", "open", "todo", "no":
			v := false
			completed = &v
		}
		items, err := client.ListTasks(ctx, completed, 50)
		if err != nil {
			return "", err
		}
		if len(items) == 0 {
			return "No Feishu tasks visible to this bot (my_tasks). If you have a todo link or guid, use feishu_get_task.", nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%d Feishu task(s):\n", len(items))
		for _, t := range items {
			b.WriteString(formatFeishuTask(t))
			b.WriteByte('\n')
		}
		return strings.TrimSpace(b.String()), nil
	}
}

func makeFeishuGetTask(st store.Store, r *Registry, agentID string) ToolFunc {
	return func(ctx context.Context, rawArgs json.RawMessage) (string, error) {
		var args feishuGetTaskArgs
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
		if strings.TrimSpace(args.TaskGUID) == "" {
			return "", fmt.Errorf("task_guid required")
		}
		client, err := feishuOfficeClient(ctx, st, r, agentID)
		if err != nil {
			return "", err
		}
		info, err := client.GetTask(ctx, args.TaskGUID)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(formatFeishuTask(info)), nil
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
		if strings.TrimSpace(args.ConfirmToken) != "" {
			p, err := feishuTakePending(agentID, args.ConfirmToken)
			if err != nil {
				return "", err
			}
			if p.Kind != "complete_task" || p.TaskGUID == "" {
				return "", fmt.Errorf("confirm_token is not for completing a task")
			}
			if err := client.CompleteTask(ctx, p.TaskGUID); err != nil {
				return "", err
			}
			return "Completed Feishu task " + p.TaskGUID + ".", nil
		}
		info, err := client.GetTask(ctx, guid)
		if err != nil {
			return "", err
		}
		preview := "About to COMPLETE this Feishu task:\n" + formatFeishuTask(info)
		tok := feishuStorePending(feishuPending{AgentID: agentID, Kind: "complete_task", TaskGUID: info.GUID, Preview: preview})
		return feishuConfirmPrompt(preview, tok), nil
	}
}

func makeFeishuUpdateTask(st store.Store, r *Registry, agentID string) ToolFunc {
	return func(ctx context.Context, rawArgs json.RawMessage) (string, error) {
		var args feishuUpdateTaskArgs
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
		if strings.TrimSpace(args.ConfirmToken) != "" {
			p, err := feishuTakePending(agentID, args.ConfirmToken)
			if err != nil {
				return "", err
			}
			if p.Kind != "update_task" {
				return "", fmt.Errorf("confirm_token is not for updating a task")
			}
			if err := client.UpdateTask(ctx, p.TaskGUID, p.Patch); err != nil {
				return "", err
			}
			return "Updated Feishu task " + p.TaskGUID + ".", nil
		}
		info, err := client.GetTask(ctx, guid)
		if err != nil {
			return "", err
		}
		tzName := scope.Timezone(ctx, st, r.ChatterUserID(), agentID)
		loc := scope.LoadLocationOrLocal(tzName)
		patch, err := feishuTaskPatchFromArgs(args, loc)
		if err != nil {
			return "", err
		}
		preview := "About to UPDATE this Feishu task:\n" + formatFeishuTask(info) + "\nProposed changes:\n" + formatFeishuTaskPatch(patch)
		tok := feishuStorePending(feishuPending{
			AgentID: agentID, Kind: "update_task", TaskGUID: info.GUID,
			Patch: patch, Preview: preview,
		})
		return feishuConfirmPrompt(preview, tok), nil
	}
}

func makeFeishuAppendDoc(st store.Store, r *Registry, agentID string) ToolFunc {
	return func(ctx context.Context, rawArgs json.RawMessage) (string, error) {
		var args feishuAppendDocArgs
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
		ref := strings.TrimSpace(args.DocumentID)
		if ref == "" {
			return "", fmt.Errorf("document_id required")
		}
		title := strings.TrimSpace(args.Title)
		content := strings.TrimSpace(args.Content)
		if title == "" && content == "" {
			return "", fmt.Errorf("title or content is required")
		}
		client, err := feishuOfficeClient(ctx, st, r, agentID)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(args.ConfirmToken) != "" {
			p, err := feishuTakePending(agentID, args.ConfirmToken)
			if err != nil {
				return "", err
			}
			if p.Kind != "append_doc" {
				return "", fmt.Errorf("confirm_token is not for modifying a document")
			}
			if p.DocTitle != "" {
				if err := client.UpdateDocTitle(ctx, p.DocID, p.DocTitle); err != nil {
					return "", err
				}
			}
			if p.DocAppend != "" {
				if err := client.AppendDocText(ctx, p.DocID, p.DocAppend); err != nil {
					return "", err
				}
			}
			return "Updated Feishu doc " + p.DocID + ".", nil
		}
		curTitle, curText, err := client.ReadDoc(ctx, ref)
		if err != nil {
			return "", err
		}
		if curTitle == "" {
			curTitle = "(untitled)"
		}
		var b strings.Builder
		b.WriteString("About to MODIFY Feishu doc ")
		b.WriteString(ref)
		b.WriteString(" (current title: ")
		b.WriteString(curTitle)
		b.WriteString(").\n")
		if title != "" {
			b.WriteString("New title: ")
			b.WriteString(title)
			b.WriteByte('\n')
		}
		if content != "" {
			b.WriteString("Append:\n")
			b.WriteString(content)
			b.WriteByte('\n')
		}
		if n := len(curText); n > 400 {
			b.WriteString("Current body (truncated): ")
			b.WriteString(curText[:400])
			b.WriteString("…\n")
		} else if n > 0 {
			b.WriteString("Current body: ")
			b.WriteString(curText)
			b.WriteByte('\n')
		}
		tok := feishuStorePending(feishuPending{
			AgentID: agentID, Kind: "append_doc", DocID: ref,
			DocTitle: title, DocAppend: content, Preview: b.String(),
		})
		return feishuConfirmPrompt(strings.TrimSpace(b.String()), tok), nil
	}
}

func feishuTaskPatchFromArgs(args feishuUpdateTaskArgs, loc *time.Location) (channels.FeishuTaskPatch, error) {
	var patch channels.FeishuTaskPatch
	if s := strings.TrimSpace(args.Summary); s != "" {
		patch.Summary = &s
	}
	if args.Description != "" {
		d := args.Description
		patch.Description = &d
	}
	if strings.TrimSpace(args.Due) != "" {
		unix, day, err := parseWeComWhen(args.Due, loc)
		if err != nil {
			return patch, fmt.Errorf("due: %w", err)
		}
		patch.SetDue = true
		patch.DueUnix = unix
		patch.WholeDay = args.WholeDay || day
	}
	if args.ClearDue {
		patch.ClearDue = true
		patch.SetDue = false
	}
	patch.Complete = args.Complete
	if patch.Summary == nil && patch.Description == nil && !patch.SetDue && !patch.ClearDue && patch.Complete == nil {
		return patch, fmt.Errorf("provide summary, description, due, clear_due, or complete")
	}
	return patch, nil
}

func formatFeishuTask(t channels.FeishuTaskInfo) string {
	status := t.Status
	if status == "" {
		if t.Completed {
			status = "done"
		} else {
			status = "todo"
		}
	}
	line := fmt.Sprintf("- %s [%s] %s", t.GUID, status, strings.TrimSpace(t.Summary))
	if t.DueUnix > 0 {
		when := time.Unix(t.DueUnix, 0).UTC().Format("2006-01-02")
		if !t.WholeDay {
			when = time.Unix(t.DueUnix, 0).UTC().Format("2006-01-02 15:04")
		}
		line += " due " + when
	}
	if u := strings.TrimSpace(t.URL); u != "" {
		line += " " + u
	}
	if d := strings.TrimSpace(t.Description); d != "" {
		if len(d) > 160 {
			d = d[:160] + "…"
		}
		line += "\n  " + d
	}
	return line
}

func formatFeishuTaskPatch(p channels.FeishuTaskPatch) string {
	var parts []string
	if p.Summary != nil {
		parts = append(parts, "summary → "+*p.Summary)
	}
	if p.Description != nil {
		parts = append(parts, "description → "+*p.Description)
	}
	if p.ClearDue {
		parts = append(parts, "due → (cleared)")
	} else if p.SetDue {
		parts = append(parts, fmt.Sprintf("due → %d (whole_day=%v)", p.DueUnix, p.WholeDay))
	}
	if p.Complete != nil {
		if *p.Complete {
			parts = append(parts, "status → done")
		} else {
			parts = append(parts, "status → todo")
		}
	}
	if len(parts) == 0 {
		return "(none)"
	}
	return "- " + strings.Join(parts, "\n- ")
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
