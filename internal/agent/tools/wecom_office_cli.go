package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fastclaw-ai/fastclaw/internal/channels"
	"github.com/fastclaw-ai/fastclaw/internal/store"
)

type wecomCLIArgs struct {
	Command string          `json:"command"`
	Args    json.RawMessage `json:"args"`
}

type wecomDomainArgs struct {
	Method string          `json:"method"`
	Args   json.RawMessage `json:"args"`
}

type wecomCreateSheetArgs struct {
	Title string          `json:"title"`
	Rows  json.RawMessage `json:"rows"`
}

type wecomGetSheetArgs struct {
	DocumentID string `json:"document_id"`
}

type wecomReadSheetArgs struct {
	DocumentID string `json:"document_id"`
	SheetID    string `json:"sheet_id"`
	Range      string `json:"range"`
}

type wecomWriteSheetArgs struct {
	DocumentID   string          `json:"document_id"`
	SheetID      string          `json:"sheet_id"`
	StartRow     int             `json:"start_row"`
	StartColumn  int             `json:"start_column"`
	Rows         json.RawMessage `json:"rows"`
	ConfirmToken string          `json:"confirm_token,omitempty"`
}

type wecomAppendSheetArgs struct {
	DocumentID string          `json:"document_id"`
	SheetID    string          `json:"sheet_id"`
	Values     json.RawMessage `json:"values"`
}

// registerWeComCLITools wires every 101750 CLI prefix. Specific calendar/doc
// tools stay registered; this adds 在线表格 first-class helpers plus a generic
// wecom_cli catch-all and per-prefix method tools.
func registerWeComCLITools(r *Registry, st store.Store, agentID string) {
	r.Register("wecom_cli",
		"Call any official 企业微信智能机器人 CLI command from https://developer.work.weixin.qq.com/document/path/101750. command is the wecom-cli path without the binary (e.g. 'sheet rows append', 'todo create', 'meeting list', 'mail search', 'smartsheet records list', 'smartpage pages get', 'disk files search', 'message aibot sessions list'). args is the JSON body for that command. Prefer the dedicated wecom_* tools when one exists. Destructive writes (delete/overwrite/cancel/send) must be confirmed with the user before calling.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{
					"type":        "string",
					"description": "CLI command, e.g. sheet create, todo list, calendar schedules free list.",
				},
				"args": map[string]interface{}{
					"description": "JSON object (or JSON string) matching that command's documented fields.",
				},
			},
			"required": []string{"command"},
		},
		makeWeComCLI(st, r, agentID),
	)

	registerWeComDomain(r, st, agentID, "wecom_sheet", "sheet",
		"企业微信在线表格 (wecom-cli sheet). method is the rest of the CLI command: create, get, import, contents update, ranges get, rows append, subsheets add, subsheets delete. args is the JSON body (docid, sheet_id, grid_data, …). Prefer wecom_create_sheet / wecom_read_sheet / wecom_append_sheet_row for common cases.")
	registerWeComDomain(r, st, agentID, "wecom_smartsheet", "smartsheet",
		"企业微信智能表格 (wecom-cli smartsheet). method examples: create, get, records list, records add, records update, records delete, records query, fields list, fields add, sheets list, views list, charts list. args is the JSON body.")
	registerWeComDomain(r, st, agentID, "wecom_smartpage", "smartpage",
		"企业微信智能文档 (wecom-cli smartpage). method examples: create, import, pages get, pages append, pages overwrite, pages update, blocks update, databases get. args is the JSON body.")
	registerWeComDomain(r, st, agentID, "wecom_todo", "todo",
		"企业微信待办 (wecom-cli todo). method: create, list, get, update, finish, delete. create args: {\"items\":[{\"title\":\"…\"}]}. Confirm with the user before finish/delete.")
	registerWeComDomain(r, st, agentID, "wecom_meeting", "meeting",
		"企业微信会议 (wecom-cli meeting). method: create, get, list, search, update, cancel, original get, rooms search, rooms buildings list. create uses subject/begin_time/end_time (YYYY-MM-DD HH:mm:ss). Confirm before cancel.")
	registerWeComDomain(r, st, agentID, "wecom_mail", "mail",
		"企业微信邮件 (wecom-cli mail). method: search, get, send. Confirm with the user before send.")
	registerWeComDomain(r, st, agentID, "wecom_disk", "disk",
		"企业微信微盘 (wecom-cli disk). method: files search, files list, files get, files rename, files upload, files download, folders create. args is the JSON body.")
	registerWeComDomain(r, st, agentID, "wecom_message", "message",
		"企业微信主动发消息 (wecom-cli message). method: aibot sessions list, aibot send. List sessions first to get chat_id, then send. This is proactive send, not the current chat reply.")
	registerWeComDomain(r, st, agentID, "wecom_media", "media",
		"企业微信素材 (wecom-cli media). method: upload, download. upload returns media_id for message/mail/disk.")

	r.Register("wecom_create_sheet",
		"Create an official 企业微信在线表格 (sheet) via the connected robot and share it with the current WeCom sender. Use for 表格 / Excel-like grids, not 在线文档 doc.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"title": map[string]interface{}{
					"type":        "string",
					"description": "Spreadsheet title.",
				},
				"rows": map[string]interface{}{
					"description": "Optional initial grid as a JSON array of rows, each row an array of cell strings. Example: [[\"姓名\",\"分数\"],[\"张三\",\"90\"]].",
				},
			},
			"required": []string{"title"},
		},
		makeWeComCreateSheet(st, r, agentID),
	)
	r.Register("wecom_get_sheet",
		"Get an official 企业微信在线表格's name, URL, and worksheet list (sheet_id / title / data_range). Use sheet_id with wecom_read_sheet / wecom_append_sheet_row.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"document_id": map[string]interface{}{
					"type":        "string",
					"description": "docid or a https://doc.weixin.qq.com/sheet/… URL.",
				},
			},
			"required": []string{"document_id"},
		},
		makeWeComGetSheet(st, r, agentID),
	)
	r.Register("wecom_read_sheet",
		"Read cell values from an official 企业微信在线表格 range (A1 notation). Call wecom_get_sheet first if you do not have sheet_id / data_range.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"document_id": map[string]interface{}{
					"type": "string", "description": "docid or /sheet/ URL.",
				},
				"sheet_id": map[string]interface{}{
					"type": "string", "description": "Worksheet id from wecom_get_sheet.",
				},
				"range": map[string]interface{}{
					"type": "string", "description": "A1 range such as A1:D20. Use the sheet's data_range when unsure.",
				},
			},
			"required": []string{"document_id", "sheet_id", "range"},
		},
		makeWeComReadSheet(st, r, agentID),
	)
	r.Register("wecom_write_sheet",
		"Write cells into an official 企业微信在线表格 starting at start_row/start_column (0-based). REQUIRES two-step confirmation: first call returns preview + confirm_token.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"document_id":  map[string]interface{}{"type": "string"},
				"sheet_id":     map[string]interface{}{"type": "string"},
				"start_row":    map[string]interface{}{"type": "integer", "description": "0-based start row."},
				"start_column": map[string]interface{}{"type": "integer", "description": "0-based start column."},
				"rows":         map[string]interface{}{"description": "JSON array of rows, each an array of cell strings."},
				"confirm_token": map[string]interface{}{
					"type": "string", "description": "Token from the preview call.",
				},
			},
			"required": []string{"document_id", "sheet_id", "rows"},
		},
		makeWeComWriteSheet(st, r, agentID),
	)
	r.Register("wecom_append_sheet_row",
		"Append one row at the bottom of an official 企业微信在线表格 worksheet.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"document_id": map[string]interface{}{"type": "string"},
				"sheet_id":    map[string]interface{}{"type": "string"},
				"values":      map[string]interface{}{"description": "JSON array of cell strings for the new row, e.g. [\"张三\",\"90\"]."},
			},
			"required": []string{"document_id", "sheet_id", "values"},
		},
		makeWeComAppendSheetRow(st, r, agentID),
	)
}

func registerWeComDomain(r *Registry, st store.Store, agentID, tool, service, desc string) {
	r.Register(tool, desc,
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"method": map[string]interface{}{
					"type":        "string",
					"description": "CLI method after '" + service + "', e.g. create, rows append, records list.",
				},
				"args": map[string]interface{}{
					"description": "JSON object (or JSON string) for that method.",
				},
			},
			"required": []string{"method"},
		},
		makeWeComDomain(st, r, agentID, service),
	)
}

func makeWeComCLI(st store.Store, r *Registry, agentID string) ToolFunc {
	return func(ctx context.Context, rawArgs json.RawMessage) (string, error) {
		var args wecomCLIArgs
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
		if strings.TrimSpace(args.Command) == "" {
			return "", fmt.Errorf("command required")
		}
		payload, err := wecomParseArgs(args.Args)
		if err != nil {
			return "", err
		}
		client, err := wecomCLIClient(ctx, st, r, agentID)
		if err != nil {
			return "", err
		}
		raw, err := client.CallCommand(ctx, args.Command, payload)
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
}

func makeWeComDomain(st store.Store, r *Registry, agentID, service string) ToolFunc {
	return func(ctx context.Context, rawArgs json.RawMessage) (string, error) {
		var args wecomDomainArgs
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
		if strings.TrimSpace(args.Method) == "" {
			return "", fmt.Errorf("method required")
		}
		payload, err := wecomParseArgs(args.Args)
		if err != nil {
			return "", err
		}
		client, err := wecomCLIClient(ctx, st, r, agentID)
		if err != nil {
			return "", err
		}
		raw, err := client.CallCommand(ctx, service+" "+args.Method, payload)
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
}

func makeWeComCreateSheet(st store.Store, r *Registry, agentID string) ToolFunc {
	return func(ctx context.Context, rawArgs json.RawMessage) (string, error) {
		var args wecomCreateSheetArgs
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
		title := strings.TrimSpace(args.Title)
		if title == "" {
			return "", fmt.Errorf("title required")
		}
		rows, err := wecomParseStringGrid(args.Rows)
		if err != nil {
			return "", err
		}
		client, err := wecomCLIClient(ctx, st, r, agentID)
		if err != nil {
			return "", err
		}
		raw, err := client.CreateSheet(ctx, title, rows)
		if err != nil {
			return "", err
		}
		id, url, name := channels.WeComCLIPickDoc(raw)
		if me := wecomSenderUserID(ctx, st, r); me != "" && id != "" {
			_ = client.ShareDoc(ctx, id, me)
		}
		if name == "" {
			name = title
		}
		if url != "" {
			return fmt.Sprintf("Created WeCom sheet [%s](%s).", name, url), nil
		}
		if id != "" {
			return fmt.Sprintf("Created WeCom sheet %s (%s).", name, id), nil
		}
		return "Created WeCom sheet " + name + ".\n" + string(raw), nil
	}
}

func makeWeComGetSheet(st store.Store, r *Registry, agentID string) ToolFunc {
	return func(ctx context.Context, rawArgs json.RawMessage) (string, error) {
		var args wecomGetSheetArgs
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
		raw, err := client.GetSheet(ctx, id)
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
}

func makeWeComReadSheet(st store.Store, r *Registry, agentID string) ToolFunc {
	return func(ctx context.Context, rawArgs json.RawMessage) (string, error) {
		var args wecomReadSheetArgs
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
		id := channels.WeComCLIDocID(args.DocumentID)
		if id == "" || strings.TrimSpace(args.SheetID) == "" {
			return "", fmt.Errorf("document_id and sheet_id required")
		}
		client, err := wecomCLIClient(ctx, st, r, agentID)
		if err != nil {
			return "", err
		}
		raw, err := client.GetSheetRange(ctx, id, args.SheetID, args.Range)
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
}

func makeWeComWriteSheet(st store.Store, r *Registry, agentID string) ToolFunc {
	return func(ctx context.Context, rawArgs json.RawMessage) (string, error) {
		var args wecomWriteSheetArgs
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
		id := channels.WeComCLIDocID(args.DocumentID)
		rows, err := wecomParseStringGrid(args.Rows)
		if err != nil {
			return "", err
		}
		if id == "" || strings.TrimSpace(args.SheetID) == "" || len(rows) == 0 {
			return "", fmt.Errorf("document_id, sheet_id, and rows required")
		}
		if strings.TrimSpace(args.ConfirmToken) != "" {
			p, err := wecomTakePending(agentID, args.ConfirmToken)
			if err != nil {
				return "", err
			}
			if p.Kind != "write_sheet" {
				return "", fmt.Errorf("confirm_token is not for writing a sheet")
			}
			id = p.DocID
			client, err := wecomCLIClient(ctx, st, r, agentID)
			if err != nil {
				return "", err
			}
			raw, err := client.UpdateSheetRange(ctx, id, p.SheetID, p.StartRow, p.StartCol, p.Rows)
			if err != nil {
				return "", err
			}
			return "Updated WeCom sheet " + id + ".\n" + string(raw), nil
		}
		preview := fmt.Sprintf("Write %d row(s) to WeCom sheet %s / %s at (%d,%d).", len(rows), id, args.SheetID, args.StartRow, args.StartColumn)
		tok := wecomStorePending(wecomPending{
			AgentID:  agentID,
			Kind:     "write_sheet",
			Preview:  preview,
			DocID:    id,
			SheetID:  args.SheetID,
			StartRow: args.StartRow,
			StartCol: args.StartColumn,
			Rows:     rows,
		})
		return wecomConfirmPrompt(preview, tok), nil
	}
}

func makeWeComAppendSheetRow(st store.Store, r *Registry, agentID string) ToolFunc {
	return func(ctx context.Context, rawArgs json.RawMessage) (string, error) {
		var args wecomAppendSheetArgs
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
		id := channels.WeComCLIDocID(args.DocumentID)
		vals, err := wecomParseStringRow(args.Values)
		if err != nil {
			return "", err
		}
		if id == "" || strings.TrimSpace(args.SheetID) == "" || len(vals) == 0 {
			return "", fmt.Errorf("document_id, sheet_id, and values required")
		}
		client, err := wecomCLIClient(ctx, st, r, agentID)
		if err != nil {
			return "", err
		}
		raw, err := client.AppendSheetRow(ctx, id, args.SheetID, vals)
		if err != nil {
			return "", err
		}
		return "Appended a row to WeCom sheet " + id + ".\n" + string(raw), nil
	}
}

func wecomParseArgs(raw json.RawMessage) (any, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]any{}, nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return map[string]any{}, nil
		}
		raw = []byte(s)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("args must be JSON: %w", err)
	}
	return v, nil
}

func wecomParseStringGrid(raw json.RawMessage) ([][]string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		raw = []byte(strings.TrimSpace(s))
		if len(raw) == 0 {
			return nil, nil
		}
	}
	var rows [][]string
	if err := json.Unmarshal(raw, &rows); err == nil {
		return rows, nil
	}
	var anyRows [][]any
	if err := json.Unmarshal(raw, &anyRows); err != nil {
		return nil, fmt.Errorf("rows must be a JSON array of arrays: %w", err)
	}
	out := make([][]string, len(anyRows))
	for i, row := range anyRows {
		out[i] = make([]string, len(row))
		for j, cell := range row {
			out[i][j] = fmt.Sprint(cell)
		}
	}
	return out, nil
}

func wecomParseStringRow(raw json.RawMessage) ([]string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil, fmt.Errorf("values required")
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		raw = []byte(strings.TrimSpace(s))
	}
	var vals []string
	if err := json.Unmarshal(raw, &vals); err == nil {
		return vals, nil
	}
	var anyVals []any
	if err := json.Unmarshal(raw, &anyVals); err != nil {
		return nil, fmt.Errorf("values must be a JSON array: %w", err)
	}
	out := make([]string, len(anyVals))
	for i, v := range anyVals {
		out[i] = fmt.Sprint(v)
	}
	return out, nil
}
