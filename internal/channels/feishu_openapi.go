package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/config"
	"github.com/fastclaw-ai/fastclaw/internal/store"
)

// Official Feishu / Lark OpenAPI client used by IM-native tools
// (calendar, task, docs). Same tenant_access_token as the Feishu
// channel — App ID + Secret from the QR bot or pasted custom app.
//
// Docs:
//
//	https://open.feishu.cn/document/server-docs/calendar-v4/calendar-event/create
//	https://open.feishu.cn/document/task-v2/task/create
//	https://open.feishu.cn/document/server-docs/docs/docs/docx-v1/document/create
const feishuOpenAPITokenSkew = 60 * time.Second

// FeishuOpenAPI is a tenant-token client. One instance per (app_id, secret).
type FeishuOpenAPI struct {
	AppID     string
	AppSecret string
	BaseURL   string
	HTTP      *http.Client

	mu          sync.Mutex
	token       string
	tokenExpiry time.Time
}

// FeishuEvent is the subset we expose for calendar create.
type FeishuEvent struct {
	Summary     string
	Description string
	Location    string
	StartUnix   int64
	EndUnix     int64
	Timezone    string
	WholeDay    bool
	Attendees   []string // open_id
	RemindMins  int      // 0 = none; minutes before start
}

// FeishuTask is the subset we expose for task v2 create.
type FeishuTask struct {
	Summary     string
	Description string
	DueUnix     int64 // seconds; 0 = none
	WholeDay    bool
	Assignees   []string // open_id
}

// FeishuTaskInfo is a task record returned by list/get.
type FeishuTaskInfo struct {
	GUID        string
	Summary     string
	Description string
	Status      string
	URL         string
	DueUnix     int64 // seconds; 0 = none
	WholeDay    bool
	Completed   bool
	Assignees   []string // open_id
}

// FeishuTaskPatch is a partial update. Nil / unset fields are left alone.
type FeishuTaskPatch struct {
	Summary     *string
	Description *string
	SetDue      bool
	ClearDue    bool
	DueUnix     int64
	WholeDay    bool
	Complete    *bool // true = done, false = reopen
}

// FeishuDoc is the subset we expose for docx create.
type FeishuDoc struct {
	Title   string
	Content string
	Share   []string // open_id collaborators (full_access)
}

// NewFeishuOpenAPI builds a client. baseURL empty → open.feishu.cn.
func NewFeishuOpenAPI(appID, secret, baseURL string) *FeishuOpenAPI {
	if baseURL == "" {
		baseURL = feishuBaseURL
	}
	return &FeishuOpenAPI{
		AppID:     strings.TrimSpace(appID),
		AppSecret: strings.TrimSpace(secret),
		BaseURL:   strings.TrimRight(baseURL, "/"),
		HTTP:      &http.Client{Timeout: 15 * time.Second},
	}
}

// FeishuOpenAPIFromChannel reads App ID + Secret off a feishu ChannelRecord.
func FeishuOpenAPIFromChannel(ch *store.ChannelRecord) (*FeishuOpenAPI, error) {
	if ch == nil {
		return nil, fmt.Errorf("feishu api: channel not bound")
	}
	cc := config.ChannelConfigFromData(ch.Data)
	acct, ok := cc.Accounts[ch.AccountID]
	if !ok {
		for _, a := range cc.Accounts {
			acct = a
			ok = true
			break
		}
	}
	secret := strings.TrimSpace(acct.BotToken)
	if secret == "" {
		secret = strings.TrimSpace(cc.BotToken)
	}
	appID := strings.TrimSpace(ch.AccountID)
	if appID == "" || secret == "" {
		return nil, fmt.Errorf("feishu api: App ID + Secret missing — connect Feishu on the Channels page")
	}
	return NewFeishuOpenAPI(appID, secret, ""), nil
}

// TenantToken returns a cached tenant_access_token.
func (c *FeishuOpenAPI) TenantToken(ctx context.Context) (string, error) {
	if c == nil || c.AppID == "" || c.AppSecret == "" {
		return "", fmt.Errorf("feishu api: app_id and app_secret required")
	}
	c.mu.Lock()
	if c.token != "" && time.Now().Before(c.tokenExpiry) {
		tok := c.token
		c.mu.Unlock()
		return tok, nil
	}
	c.mu.Unlock()

	body, _ := json.Marshal(map[string]string{
		"app_id":     c.AppID,
		"app_secret": c.AppSecret,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/open-apis/auth/v3/tenant_access_token/internal", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	var parsed struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int    `json:"expire"`
	}
	if err := c.doJSON(req, &parsed); err != nil {
		return "", err
	}
	if parsed.Code != 0 || parsed.TenantAccessToken == "" {
		return "", feishuAPIErr("tenant_access_token", parsed.Code, parsed.Msg)
	}
	ttl := time.Duration(parsed.Expire) * time.Second
	if ttl <= 0 {
		ttl = 2 * time.Hour
	}
	if ttl > feishuOpenAPITokenSkew {
		ttl -= feishuOpenAPITokenSkew
	}
	c.mu.Lock()
	c.token = parsed.TenantAccessToken
	c.tokenExpiry = time.Now().Add(ttl)
	c.mu.Unlock()
	return parsed.TenantAccessToken, nil
}

// PrimaryCalendarID returns the current identity's primary calendar
// (the bot's primary calendar when using tenant_access_token).
func (c *FeishuOpenAPI) PrimaryCalendarID(ctx context.Context) (string, error) {
	var parsed struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Calendars []struct {
				Calendar struct {
					CalendarID string `json:"calendar_id"`
				} `json:"calendar"`
			} `json:"calendars"`
		} `json:"data"`
	}
	if err := c.call(ctx, http.MethodPost, "/open-apis/calendar/v4/calendars/primary?user_id_type=open_id", nil, &parsed); err != nil {
		return "", err
	}
	if parsed.Code != 0 {
		return "", feishuAPIErr("calendars/primary", parsed.Code, parsed.Msg)
	}
	if len(parsed.Data.Calendars) == 0 || parsed.Data.Calendars[0].Calendar.CalendarID == "" {
		return "", fmt.Errorf("feishu api: empty primary calendar — disconnect the bot and scan again so calendar scopes take effect")
	}
	return parsed.Data.Calendars[0].Calendar.CalendarID, nil
}

// CreateEvent writes a calendar event on the bot primary calendar,
// then invites attendees (open_id). Returns event_id and an applink.
func (c *FeishuOpenAPI) CreateEvent(ctx context.Context, ev FeishuEvent) (eventID, calID, link string, err error) {
	if ev.StartUnix <= 0 || ev.EndUnix <= 0 {
		return "", "", "", fmt.Errorf("feishu api: start and end time required")
	}
	if ev.EndUnix <= ev.StartUnix {
		return "", "", "", fmt.Errorf("feishu api: end must be after start")
	}
	calID, err = c.PrimaryCalendarID(ctx)
	if err != nil {
		return "", "", "", err
	}
	tz := strings.TrimSpace(ev.Timezone)
	if tz == "" {
		tz = "Asia/Shanghai"
	}
	summary := strings.TrimSpace(ev.Summary)
	if summary == "" {
		summary = "日程"
	}
	body := map[string]any{
		"summary":           summary,
		"need_notification": true,
		"attendee_ability":  "can_see_others",
		"visibility":        "default",
		"free_busy_status":  "busy",
		"start_time":        feishuEventTime(ev.StartUnix, tz, ev.WholeDay),
		"end_time":          feishuEventTime(ev.EndUnix, tz, ev.WholeDay),
	}
	if d := strings.TrimSpace(ev.Description); d != "" {
		body["description"] = d
	}
	if loc := strings.TrimSpace(ev.Location); loc != "" {
		body["location"] = map[string]string{"name": loc}
	}
	if ev.RemindMins > 0 {
		body["reminders"] = []map[string]int{{"minutes": ev.RemindMins}}
	}
	var parsed struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Event struct {
				EventID string `json:"event_id"`
				AppLink string `json:"app_link"`
			} `json:"event"`
		} `json:"data"`
	}
	path := "/open-apis/calendar/v4/calendars/" + url.PathEscape(calID) + "/events"
	if err := c.call(ctx, http.MethodPost, path, body, &parsed); err != nil {
		return "", "", "", err
	}
	if parsed.Code != 0 {
		return "", "", "", feishuAPIErr("calendar/events", parsed.Code, parsed.Msg)
	}
	eventID = parsed.Data.Event.EventID
	if eventID == "" {
		return "", "", "", fmt.Errorf("feishu api: empty event_id")
	}
	link = parsed.Data.Event.AppLink
	if len(ev.Attendees) > 0 {
		if err := c.AddEventAttendees(ctx, calID, eventID, ev.Attendees); err != nil {
			return eventID, calID, link, fmt.Errorf("created event %s but inviting attendees failed: %w", eventID, err)
		}
	}
	return eventID, calID, link, nil
}

// AddEventAttendees invites users (open_id) onto an event.
func (c *FeishuOpenAPI) AddEventAttendees(ctx context.Context, calID, eventID string, openIDs []string) error {
	atts := make([]map[string]string, 0, len(openIDs))
	seen := map[string]bool{}
	for _, id := range openIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		atts = append(atts, map[string]string{"type": "user", "user_id": id})
	}
	if len(atts) == 0 {
		return nil
	}
	var parsed struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	path := "/open-apis/calendar/v4/calendars/" + url.PathEscape(calID) +
		"/events/" + url.PathEscape(eventID) + "/attendees?user_id_type=open_id"
	if err := c.call(ctx, http.MethodPost, path, map[string]any{
		"attendees":         atts,
		"need_notification": true,
	}, &parsed); err != nil {
		return err
	}
	if parsed.Code != 0 {
		return feishuAPIErr("calendar/attendees", parsed.Code, parsed.Msg)
	}
	return nil
}

// CreateTask creates a task v2 item and assigns members. Returns guid + url.
func (c *FeishuOpenAPI) CreateTask(ctx context.Context, t FeishuTask) (guid, link string, err error) {
	summary := strings.TrimSpace(t.Summary)
	if summary == "" {
		return "", "", fmt.Errorf("feishu api: task summary required")
	}
	body := map[string]any{"summary": summary}
	if d := strings.TrimSpace(t.Description); d != "" {
		body["description"] = d
	}
	if t.DueUnix > 0 {
		body["due"] = map[string]any{
			"timestamp":  strconv.FormatInt(t.DueUnix*1000, 10),
			"is_all_day": t.WholeDay,
		}
	}
	if mems := feishuTaskMembers(t.Assignees); len(mems) > 0 {
		body["members"] = mems
	}
	var parsed struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Task struct {
				GUID string `json:"guid"`
				URL  string `json:"url"`
			} `json:"task"`
		} `json:"data"`
	}
	if err := c.call(ctx, http.MethodPost, "/open-apis/task/v2/tasks?user_id_type=open_id", body, &parsed); err != nil {
		return "", "", err
	}
	if parsed.Code != 0 {
		return "", "", feishuAPIErr("task/create", parsed.Code, parsed.Msg)
	}
	if parsed.Data.Task.GUID == "" {
		return "", "", fmt.Errorf("feishu api: empty task guid")
	}
	return parsed.Data.Task.GUID, parsed.Data.Task.URL, nil
}

// ListTasks returns tasks this app created. Official task v2 list
// (type=my_tasks) requires a user_access_token and is empty for a
// tenant / bot token — FastClaw only has the latter. Task v1 list
// with tenant_access_token is the documented way to read todos the
// bot created via feishu_create_task. v2 my_tasks is a fallback.
func (c *FeishuOpenAPI) ListTasks(ctx context.Context, completed *bool, pageSize int) ([]FeishuTaskInfo, error) {
	items, err := c.listTasksV1(ctx, completed, pageSize)
	if err == nil {
		return items, nil
	}
	v2, err2 := c.listTasksV2(ctx, completed, pageSize)
	if err2 == nil && len(v2) > 0 {
		return v2, nil
	}
	return nil, err
}

func (c *FeishuOpenAPI) listTasksV1(ctx context.Context, completed *bool, pageSize int) ([]FeishuTaskInfo, error) {
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 50
	}
	var out []FeishuTaskInfo
	pageToken := ""
	for pages := 0; pages < 5 && len(out) < pageSize; pages++ {
		q := url.Values{}
		q.Set("page_size", strconv.Itoa(pageSize))
		q.Set("user_id_type", "open_id")
		if pageToken != "" {
			q.Set("page_token", pageToken)
		}
		if completed != nil {
			if *completed {
				q.Set("task_completed", "true")
			} else {
				q.Set("task_completed", "false")
			}
		}
		var parsed struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
			Data struct {
				Items     []feishuTaskV1JSON `json:"items"`
				PageToken string             `json:"page_token"`
				HasMore   bool               `json:"has_more"`
			} `json:"data"`
		}
		if err := c.call(ctx, http.MethodGet, "/open-apis/task/v1/tasks?"+q.Encode(), nil, &parsed); err != nil {
			return nil, err
		}
		if parsed.Code != 0 {
			return nil, feishuAPIErr("task/v1/list", parsed.Code, parsed.Msg)
		}
		for _, raw := range parsed.Data.Items {
			out = append(out, raw.info())
			if len(out) >= pageSize {
				break
			}
		}
		if !parsed.Data.HasMore || parsed.Data.PageToken == "" {
			break
		}
		pageToken = parsed.Data.PageToken
	}
	return out, nil
}

func (c *FeishuOpenAPI) listTasksV2(ctx context.Context, completed *bool, pageSize int) ([]FeishuTaskInfo, error) {
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 50
	}
	q := url.Values{}
	q.Set("page_size", strconv.Itoa(pageSize))
	q.Set("type", "my_tasks")
	q.Set("user_id_type", "open_id")
	if completed != nil {
		if *completed {
			q.Set("completed", "true")
		} else {
			q.Set("completed", "false")
		}
	}
	var parsed struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Items []feishuTaskJSON `json:"items"`
		} `json:"data"`
	}
	if err := c.call(ctx, http.MethodGet, "/open-apis/task/v2/tasks?"+q.Encode(), nil, &parsed); err != nil {
		return nil, err
	}
	if parsed.Code != 0 {
		return nil, feishuAPIErr("task/list", parsed.Code, parsed.Msg)
	}
	out := make([]FeishuTaskInfo, 0, len(parsed.Data.Items))
	for _, raw := range parsed.Data.Items {
		out = append(out, raw.info())
	}
	return out, nil
}

// GetTask fetches one task by guid (or a Feishu todo applink containing guid=).
func (c *FeishuOpenAPI) GetTask(ctx context.Context, guid string) (FeishuTaskInfo, error) {
	var zero FeishuTaskInfo
	guid = feishuTaskGUID(guid)
	if guid == "" {
		return zero, fmt.Errorf("feishu api: task guid required")
	}
	info, err := c.getTaskV2(ctx, guid)
	if err == nil {
		return info, nil
	}
	info, err1 := c.getTaskV1(ctx, guid)
	if err1 == nil {
		return info, nil
	}
	return zero, err
}

func (c *FeishuOpenAPI) getTaskV2(ctx context.Context, guid string) (FeishuTaskInfo, error) {
	var zero FeishuTaskInfo
	var parsed struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Task feishuTaskJSON `json:"task"`
		} `json:"data"`
	}
	path := "/open-apis/task/v2/tasks/" + url.PathEscape(guid) + "?user_id_type=open_id"
	if err := c.call(ctx, http.MethodGet, path, nil, &parsed); err != nil {
		return zero, err
	}
	if parsed.Code != 0 {
		return zero, feishuAPIErr("task/get", parsed.Code, parsed.Msg)
	}
	info := parsed.Data.Task.info()
	if info.GUID == "" {
		info.GUID = guid
	}
	return info, nil
}

func (c *FeishuOpenAPI) getTaskV1(ctx context.Context, guid string) (FeishuTaskInfo, error) {
	var zero FeishuTaskInfo
	var parsed struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Task feishuTaskV1JSON `json:"task"`
		} `json:"data"`
	}
	path := "/open-apis/task/v1/tasks/" + url.PathEscape(guid) + "?user_id_type=open_id"
	if err := c.call(ctx, http.MethodGet, path, nil, &parsed); err != nil {
		return zero, err
	}
	if parsed.Code != 0 {
		return zero, feishuAPIErr("task/v1/get", parsed.Code, parsed.Msg)
	}
	info := parsed.Data.Task.info()
	if info.GUID == "" {
		info.GUID = guid
	}
	return info, nil
}

// FilterTasksByAssignee keeps tasks assigned to openID. Tasks with no
// assignees stay (bot-created, not yet assigned). Empty openID = no filter.
func FilterTasksByAssignee(items []FeishuTaskInfo, openID string) []FeishuTaskInfo {
	openID = strings.TrimSpace(openID)
	if openID == "" || len(items) == 0 {
		return items
	}
	out := make([]FeishuTaskInfo, 0, len(items))
	for _, t := range items {
		if len(t.Assignees) == 0 {
			out = append(out, t)
			continue
		}
		for _, a := range t.Assignees {
			if a == openID {
				out = append(out, t)
				break
			}
		}
	}
	return out
}

// CompleteTask marks a task finished.
func (c *FeishuOpenAPI) CompleteTask(ctx context.Context, guid string) error {
	done := true
	return c.UpdateTask(ctx, guid, FeishuTaskPatch{Complete: &done})
}

// UpdateTask patches title / notes / due / completed via task v2.
func (c *FeishuOpenAPI) UpdateTask(ctx context.Context, guid string, patch FeishuTaskPatch) error {
	guid = feishuTaskGUID(guid)
	if guid == "" {
		return fmt.Errorf("feishu api: task guid required")
	}
	task := map[string]any{}
	fields := []string{}
	if patch.Summary != nil {
		fields = append(fields, "summary")
		task["summary"] = strings.TrimSpace(*patch.Summary)
	}
	if patch.Description != nil {
		fields = append(fields, "description")
		task["description"] = *patch.Description
	}
	if patch.ClearDue {
		fields = append(fields, "due")
	} else if patch.SetDue && patch.DueUnix > 0 {
		fields = append(fields, "due")
		task["due"] = map[string]any{
			"timestamp":  strconv.FormatInt(patch.DueUnix*1000, 10),
			"is_all_day": patch.WholeDay,
		}
	}
	if patch.Complete != nil {
		fields = append(fields, "completed_at")
		if *patch.Complete {
			task["completed_at"] = strconv.FormatInt(time.Now().UnixMilli(), 10)
		} else {
			task["completed_at"] = "0"
		}
	}
	if len(fields) == 0 {
		return fmt.Errorf("feishu api: nothing to update")
	}
	var parsed struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	path := "/open-apis/task/v2/tasks/" + url.PathEscape(guid) + "?user_id_type=open_id"
	if err := c.call(ctx, http.MethodPatch, path, map[string]any{
		"task":          task,
		"update_fields": fields,
	}, &parsed); err != nil {
		return err
	}
	if parsed.Code != 0 {
		return feishuAPIErr("task/update", parsed.Code, parsed.Msg)
	}
	return nil
}

// CreateDoc creates a docx, optionally fills body text, and shares it.
func (c *FeishuOpenAPI) CreateDoc(ctx context.Context, doc FeishuDoc) (docID, link string, err error) {
	title := strings.TrimSpace(doc.Title)
	if title == "" {
		title = "未命名文档"
	}
	var parsed struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Document struct {
				DocumentID string `json:"document_id"`
				Title      string `json:"title"`
			} `json:"document"`
		} `json:"data"`
	}
	if err := c.call(ctx, http.MethodPost, "/open-apis/docx/v1/documents", map[string]any{
		"title": title,
	}, &parsed); err != nil {
		return "", "", err
	}
	if parsed.Code != 0 {
		return "", "", feishuAPIErr("docx/create", parsed.Code, parsed.Msg)
	}
	docID = parsed.Data.Document.DocumentID
	if docID == "" {
		return "", "", fmt.Errorf("feishu api: empty document_id")
	}
	link = "https://feishu.cn/docx/" + docID
	if content := strings.TrimSpace(doc.Content); content != "" {
		if err := c.AppendDocText(ctx, docID, content); err != nil {
			return docID, link, fmt.Errorf("created doc %s but writing body failed: %w", docID, err)
		}
	}
	for _, oid := range doc.Share {
		oid = strings.TrimSpace(oid)
		if oid == "" {
			continue
		}
		if err := c.ShareDoc(ctx, docID, oid); err != nil {
			return docID, link, fmt.Errorf("created doc %s but sharing failed: %w", docID, err)
		}
	}
	return docID, link, nil
}

// ShareDoc adds a user collaborator (full_access) so they can open the doc.
func (c *FeishuOpenAPI) ShareDoc(ctx context.Context, docID, openID string) error {
	var parsed struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	path := "/open-apis/drive/v1/permissions/" + url.PathEscape(docID) + "/members?type=docx"
	if err := c.call(ctx, http.MethodPost, path, map[string]any{
		"member_type": "openid",
		"member_id":   openID,
		"perm":        "full_access",
	}, &parsed); err != nil {
		return err
	}
	if parsed.Code != 0 {
		return feishuAPIErr("drive/share", parsed.Code, parsed.Msg)
	}
	return nil
}

// ReadDoc fetches title + raw text for a document_id (or a /docx/ URL).
func (c *FeishuOpenAPI) ReadDoc(ctx context.Context, documentRef string) (title, text string, err error) {
	docID := feishuDocumentID(documentRef)
	if docID == "" {
		return "", "", fmt.Errorf("feishu api: document_id required")
	}
	var meta struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Document struct {
				Title string `json:"title"`
			} `json:"document"`
		} `json:"data"`
	}
	if err := c.call(ctx, http.MethodGet, "/open-apis/docx/v1/documents/"+url.PathEscape(docID), nil, &meta); err != nil {
		return "", "", err
	}
	if meta.Code != 0 {
		return "", "", feishuAPIErr("docx/get", meta.Code, meta.Msg)
	}
	var raw struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Content string `json:"content"`
		} `json:"data"`
	}
	if err := c.call(ctx, http.MethodGet, "/open-apis/docx/v1/documents/"+url.PathEscape(docID)+"/raw_content", nil, &raw); err != nil {
		return "", "", err
	}
	if raw.Code != 0 {
		return "", "", feishuAPIErr("docx/raw_content", raw.Code, raw.Msg)
	}
	return meta.Data.Document.Title, raw.Data.Content, nil
}

// UpdateDocTitle changes an existing docx title.
func (c *FeishuOpenAPI) UpdateDocTitle(ctx context.Context, documentRef, title string) error {
	docID := feishuDocumentID(documentRef)
	title = strings.TrimSpace(title)
	if docID == "" || title == "" {
		return fmt.Errorf("feishu api: document_id and title required")
	}
	var parsed struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := c.call(ctx, http.MethodPatch, "/open-apis/docx/v1/documents/"+url.PathEscape(docID), map[string]any{
		"title": title,
	}, &parsed); err != nil {
		return err
	}
	if parsed.Code != 0 {
		return feishuAPIErr("docx/patch", parsed.Code, parsed.Msg)
	}
	return nil
}

// AppendDocText appends plain-text paragraphs to an existing docx.
func (c *FeishuOpenAPI) AppendDocText(ctx context.Context, documentRef, content string) error {
	return c.appendDocText(ctx, feishuDocumentID(documentRef), content)
}

func (c *FeishuOpenAPI) appendDocText(ctx context.Context, docID, content string) error {
	paras := strings.Split(content, "\n")
	children := make([]map[string]any, 0, len(paras))
	for _, p := range paras {
		p = strings.TrimRight(p, "\r")
		children = append(children, map[string]any{
			"block_type": 2,
			"text": map[string]any{
				"elements": []map[string]any{
					{"text_run": map[string]any{"content": p}},
				},
			},
		})
	}
	var parsed struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	path := "/open-apis/docx/v1/documents/" + url.PathEscape(docID) +
		"/blocks/" + url.PathEscape(docID) + "/children"
	if err := c.call(ctx, http.MethodPost, path, map[string]any{"children": children}, &parsed); err != nil {
		return err
	}
	if parsed.Code != 0 {
		return feishuAPIErr("docx/blocks", parsed.Code, parsed.Msg)
	}
	return nil
}

func (c *FeishuOpenAPI) call(ctx context.Context, method, path string, body any, dest any) error {
	tok, err := c.TenantToken(ctx)
	if err != nil {
		return err
	}
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if body != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	return c.doJSON(req, dest)
}

func (c *FeishuOpenAPI) doJSON(req *http.Request, dest any) error {
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("feishu api: %w", err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return fmt.Errorf("feishu api: read body: %w", err)
	}
	if resp.StatusCode >= 400 {
		return feishuHTTPErr(resp.StatusCode, payload)
	}
	if err := json.Unmarshal(payload, dest); err != nil {
		return fmt.Errorf("feishu api: decode: %w", err)
	}
	return nil
}

func feishuEventTime(unix int64, tz string, wholeDay bool) map[string]string {
	if wholeDay {
		loc := time.UTC
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
		return map[string]string{"date": time.Unix(unix, 0).In(loc).Format("2006-01-02")}
	}
	return map[string]string{
		"timestamp": strconv.FormatInt(unix, 10),
		"timezone":  tz,
	}
}

func feishuTaskMembers(openIDs []string) []map[string]string {
	out := make([]map[string]string, 0, len(openIDs))
	seen := map[string]bool{}
	for _, id := range openIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, map[string]string{"id": id, "type": "user", "role": "assignee"})
	}
	return out
}

type feishuTaskJSON struct {
	GUID        string `json:"guid"`
	Summary     string `json:"summary"`
	Description string `json:"description"`
	Status      string `json:"status"`
	URL         string `json:"url"`
	CompletedAt string `json:"completed_at"`
	Due         struct {
		Timestamp string `json:"timestamp"`
		IsAllDay  bool   `json:"is_all_day"`
	} `json:"due"`
	Members []struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	} `json:"members"`
}

func (t feishuTaskJSON) info() FeishuTaskInfo {
	info := FeishuTaskInfo{
		GUID:        t.GUID,
		Summary:     t.Summary,
		Description: t.Description,
		Status:      t.Status,
		URL:         t.URL,
		WholeDay:    t.Due.IsAllDay,
	}
	if ms, err := strconv.ParseInt(strings.TrimSpace(t.Due.Timestamp), 10, 64); err == nil && ms > 0 {
		info.DueUnix = ms / 1000
	}
	if t.CompletedAt != "" && t.CompletedAt != "0" {
		info.Completed = true
		if info.Status == "" {
			info.Status = "done"
		}
	}
	if info.Status == "" {
		if info.Completed {
			info.Status = "done"
		} else {
			info.Status = "todo"
		}
	}
	seen := map[string]bool{}
	for _, m := range t.Members {
		id := strings.TrimSpace(m.ID)
		if id == "" || seen[id] {
			continue
		}
		if role := strings.ToLower(strings.TrimSpace(m.Role)); role != "" && role != "assignee" {
			continue
		}
		seen[id] = true
		info.Assignees = append(info.Assignees, id)
	}
	return info
}

type feishuTaskV1JSON struct {
	ID           string `json:"id"`
	Summary      string `json:"summary"`
	Description  string `json:"description"`
	CompleteTime string `json:"complete_time"`
	Due          struct {
		Time     string `json:"time"`
		Timezone string `json:"timezone"`
		IsAllDay bool   `json:"is_all_day"`
	} `json:"due"`
	Collaborators []struct {
		ID     string   `json:"id"`
		IDList []string `json:"id_list"`
	} `json:"collaborators"`
	CollaboratorIDs []string `json:"collaborator_ids"`
}

func (t feishuTaskV1JSON) info() FeishuTaskInfo {
	id := strings.TrimSpace(t.ID)
	info := FeishuTaskInfo{
		GUID:        id,
		Summary:     t.Summary,
		Description: t.Description,
		WholeDay:    t.Due.IsAllDay,
	}
	if id != "" {
		info.URL = "https://applink.feishu.cn/client/todo/detail?guid=" + id
	}
	if sec, err := strconv.ParseInt(strings.TrimSpace(t.Due.Time), 10, 64); err == nil && sec > 0 {
		info.DueUnix = sec
	}
	if t.CompleteTime != "" && t.CompleteTime != "0" {
		info.Completed = true
		info.Status = "done"
	} else {
		info.Status = "todo"
	}
	seen := map[string]bool{}
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" || seen[raw] {
			return
		}
		seen[raw] = true
		info.Assignees = append(info.Assignees, raw)
	}
	for _, c := range t.Collaborators {
		add(c.ID)
		for _, id := range c.IDList {
			add(id)
		}
	}
	for _, id := range t.CollaboratorIDs {
		add(id)
	}
	return info
}

func feishuTaskGUID(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if u, err := url.Parse(ref); err == nil && u.RawQuery != "" {
		if g := strings.TrimSpace(u.Query().Get("guid")); g != "" {
			return g
		}
	}
	if i := strings.Index(ref, "guid="); i >= 0 {
		g := ref[i+5:]
		if j := strings.IndexAny(g, "&?#"); j >= 0 {
			g = g[:j]
		}
		if g != "" {
			return g
		}
	}
	return ref
}

func feishuDocumentID(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if i := strings.Index(ref, "/docx/"); i >= 0 {
		rest := ref[i+len("/docx/"):]
		if j := strings.IndexAny(rest, "?#/"); j >= 0 {
			rest = rest[:j]
		}
		return rest
	}
	return ref
}

func feishuHTTPErr(status int, payload []byte) error {
	var parsed struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if json.Unmarshal(payload, &parsed) == nil && (parsed.Code != 0 || parsed.Msg != "") {
		return feishuAPIErr("http", parsed.Code, parsed.Msg)
	}
	return fmt.Errorf("feishu api: HTTP %d: %s", status, strings.TrimSpace(string(payload)))
}

func feishuAPIErr(op string, code int, msg string) error {
	msg = strings.TrimSpace(msg)
	hint := feishuScopeHint(code, msg)
	if msg == "" {
		return fmt.Errorf("feishu api %s: %d%s", op, code, hint)
	}
	return fmt.Errorf("feishu api %s: %d %s%s", op, code, msg, hint)
}

func feishuScopeHint(code int, msg string) string {
	switch code {
	case 99991663, 99991664, 99991672, 191002, 1470403, 1770032:
		return " — missing Feishu scope. Disconnect this bot on Channels and scan again so 日程 / 待办 / 文档 permissions take effect (existing bots keep the old list until you rescan)."
	}
	low := strings.ToLower(msg)
	if strings.Contains(low, "scope") || strings.Contains(low, "forbidden") ||
		strings.Contains(low, "access_role") || strings.Contains(msg, "权限") {
		return " — missing Feishu scope. Disconnect this bot on Channels and scan again so 日程 / 待办 / 文档 permissions take effect."
	}
	return ""
}
