package channels

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/config"
	"github.com/fastclaw-ai/fastclaw/internal/store"
)

// Official 智能机器人 CLI/MCP gateway (not 自建应用 OA).
//
// Auth: POST cgi-bin/aibot/cli/get_cli_config with
//
//	sha256_hex(secret + bot_id + time + nonce)
//
// then Bearer token against https://qyapi.weixin.qq.com/cli
// (service discovery + calendar/doc methods). Same BotID + Secret as
// the IM long-conn. Docs: path/101753 (日程) and CLI 概述.
const (
	wecomCLIAuthURL   = "https://qyapi.weixin.qq.com/cgi-bin/aibot/cli/get_cli_config"
	wecomCLIBaseURL   = "https://qyapi.weixin.qq.com/cli"
	wecomCLIBindSrc   = 1 // Interactive
	wecomCLITokenExp  = 853004
	wecomCLIPollEvery = 400 * time.Millisecond
	wecomCLIPollMax   = 40
)

// WeComCLI is the intelligent-robot office client (calendar / docs).
type WeComCLI struct {
	BotID   string
	Secret  string
	AuthURL string
	BaseURL string
	HTTP    *http.Client

	mu         sync.Mutex
	token      string
	methods    map[string]wecomCLIRoute
	discovered bool
}

type wecomCLIRoute struct {
	Base string
	Path string
}

// WeComCLISchedule is the CLI calendar payload (wall clock, not unix).
type WeComCLISchedule struct {
	Subject     string
	BeginTime   string // YYYY-MM-DD HH:mm:ss
	EndTime     string
	Description string
	Location    string
	Attendees   []string // userids
	WholeDay    bool
	RemindSecs  int // seconds before start; 0 = omit
}

// NewWeComCLI builds a client. Empty URLs use the public qyapi hosts.
func NewWeComCLI(botID, secret string) *WeComCLI {
	return &WeComCLI{
		BotID:   strings.TrimSpace(botID),
		Secret:  strings.TrimSpace(secret),
		AuthURL: wecomCLIAuthURL,
		BaseURL: wecomCLIBaseURL,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
		methods: map[string]wecomCLIRoute{},
	}
}

// WeComCLIFromChannel reads BotID + long-conn Secret off a wecom row.
func WeComCLIFromChannel(ch *store.ChannelRecord) (*WeComCLI, error) {
	if ch == nil {
		return nil, fmt.Errorf("wecom: channel not bound")
	}
	botID := strings.TrimSpace(ch.AccountID)
	secret := strings.TrimSpace(ch.BotToken)
	if secret == "" {
		cc := config.ChannelConfigFromData(ch.Data)
		if acct, ok := cc.Accounts[ch.AccountID]; ok {
			secret = strings.TrimSpace(acct.BotToken)
		}
	}
	if botID == "" || secret == "" {
		return nil, fmt.Errorf("wecom: bot is not connected — scan or paste BotID + long-conn Secret first")
	}
	return NewWeComCLI(botID, secret), nil
}

func wecomCLISign(secret, botID string, unix int64, nonce string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s%s%d%s", secret, botID, unix, nonce)))
	return hex.EncodeToString(sum[:])
}

func wecomCLINonce() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("cli_%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("cli_%d_%s", time.Now().UnixMilli(), hex.EncodeToString(b[:]))
}

func (c *WeComCLI) http() *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *WeComCLI) authURL() string {
	if c != nil && strings.TrimSpace(c.AuthURL) != "" {
		return strings.TrimSpace(c.AuthURL)
	}
	return wecomCLIAuthURL
}

func (c *WeComCLI) baseURL() string {
	if c != nil && strings.TrimSpace(c.BaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	}
	return wecomCLIBaseURL
}

func (c *WeComCLI) AccessToken(ctx context.Context) (string, error) {
	if c == nil || c.BotID == "" || c.Secret == "" {
		return "", fmt.Errorf("wecom: botId and secret required")
	}
	c.mu.Lock()
	tok := c.token
	c.mu.Unlock()
	if tok != "" {
		return tok, nil
	}
	return c.refreshToken(ctx)
}

func (c *WeComCLI) refreshToken(ctx context.Context) (string, error) {
	now := time.Now().Unix()
	nonce := wecomCLINonce()
	body := map[string]any{
		"bot_id":      c.BotID,
		"time":        now,
		"nonce":       nonce,
		"signature":   wecomCLISign(c.Secret, c.BotID, now, nonce),
		"bind_source": wecomCLIBindSrc,
	}
	raw, err := c.postJSON(ctx, c.authURL(), body, "")
	if err != nil {
		return "", err
	}
	var parsed struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		Token   string `json:"token"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("wecom cli auth: decode: %w", err)
	}
	if parsed.ErrCode != 0 {
		return "", wecomCLIErr("get_cli_config", parsed.ErrCode, parsed.ErrMsg)
	}
	if strings.TrimSpace(parsed.Token) == "" {
		return "", fmt.Errorf("wecom cli auth: empty token")
	}
	c.mu.Lock()
	c.token = parsed.Token
	c.mu.Unlock()
	return parsed.Token, nil
}

// Invoke POSTs a CLI method. path is like /schedules/create (or a
// full URL). Body is the inner JSON object; this wraps it as
// {"payload":"<string>"} and unwraps the nested gateway envelope.
func (c *WeComCLI) Invoke(ctx context.Context, path string, payload any) (json.RawMessage, error) {
	tok, err := c.AccessToken(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := c.invokeOnce(ctx, path, payload, tok)
	if err != nil && wecomCLIIsTokenExpired(err) {
		if tok, err2 := c.refreshToken(ctx); err2 == nil {
			return c.invokeOnce(ctx, path, payload, tok)
		}
	}
	return raw, err
}

func (c *WeComCLI) invokeOnce(ctx context.Context, path string, payload any, token string) (json.RawMessage, error) {
	inner, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if string(inner) == "null" {
		inner = []byte("{}")
	}
	url := path
	if !strings.HasPrefix(path, "http://") && !strings.HasPrefix(path, "https://") {
		url = c.baseURL() + path
		if !strings.HasPrefix(path, "/") {
			url = c.baseURL() + "/" + path
		}
	}
	raw, err := c.postJSON(ctx, url, map[string]string{"payload": string(inner)}, token)
	if err != nil {
		return nil, err
	}
	result, taskID, err := wecomCLIUnwrap(url, raw)
	if err != nil {
		return nil, err
	}
	if taskID != "" && wecomCLIEmptyResult(result) {
		return c.pollTask(ctx, taskID, token)
	}
	return result, nil
}

func wecomCLIEmptyResult(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s == "" || s == "null" || s == "{}" || s == `""`
}

func (c *WeComCLI) pollTask(ctx context.Context, taskID, token string) (json.RawMessage, error) {
	for i := 0; i < wecomCLIPollMax; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wecomCLIPollEvery):
			}
		}
		inner, err := json.Marshal(map[string]string{"taskid": taskID})
		if err != nil {
			return nil, err
		}
		raw, err := c.postJSON(ctx, c.baseURL()+"/task/query", map[string]string{"payload": string(inner)}, token)
		if err != nil {
			return nil, err
		}
		result, next, err := wecomCLIUnwrap(c.baseURL()+"/task/query", raw)
		if err != nil {
			return nil, err
		}
		if !wecomCLIEmptyResult(result) {
			return result, nil
		}
		if next != "" {
			taskID = next
		}
	}
	return nil, fmt.Errorf("wecom cli: timed out waiting for task %s", taskID)
}

func (c *WeComCLI) postJSON(ctx context.Context, url string, body any, bearer string) ([]byte, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, fmt.Errorf("wecom cli: %w", err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("wecom cli: read body: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("wecom cli: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	return payload, nil
}

func wecomCLIUnwrap(url string, raw []byte) (json.RawMessage, string, error) {
	var outer struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		ResultsJSON string `json:"results_json"`
	}
	if err := json.Unmarshal(raw, &outer); err != nil {
		return nil, "", fmt.Errorf("wecom cli: decode envelope: %w", err)
	}
	if outer.ErrCode != 0 {
		return nil, "", wecomCLIErr(url, outer.ErrCode, outer.ErrMsg)
	}
	if strings.TrimSpace(outer.ResultsJSON) == "" {
		// Auth-style flat body (no results_json). Return as-is.
		return json.RawMessage(raw), "", nil
	}
	var inner struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		TaskID string `json:"taskid"`
	}
	if err := json.Unmarshal([]byte(outer.ResultsJSON), &inner); err != nil {
		return nil, "", fmt.Errorf("wecom cli: decode results_json: %w", err)
	}
	if inner.Error != nil && inner.Error.Code != 0 {
		return nil, "", wecomCLIErr(url, inner.Error.Code, inner.Error.Message)
	}
	return wecomCLIUnwrapResult(inner.Result), inner.TaskID, nil
}

func wecomCLIUnwrapResult(raw json.RawMessage) json.RawMessage {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage(`{}`)
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			s = strings.TrimSpace(s)
			if s == "" {
				return json.RawMessage(`{}`)
			}
			return json.RawMessage(s)
		}
	}
	return raw
}

func wecomCLIErr(op string, code int, msg string) error {
	if strings.TrimSpace(msg) == "" {
		msg = "unknown error"
	}
	return fmt.Errorf("wecom cli %s: %d %s", op, code, msg)
}

func wecomCLIIsTokenExpired(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), fmt.Sprintf("%d ", wecomCLITokenExp))
}

func (c *WeComCLI) Call(ctx context.Context, key string, payload any) (json.RawMessage, error) {
	if err := c.ensureMethods(ctx); err != nil {
		return nil, err
	}
	c.mu.Lock()
	rt, ok := c.methods[key]
	c.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("wecom cli: method %s not found (authorize 可使用权限 for this bot, then retry)", key)
	}
	path := rt.Path
	if rt.Base != "" && !strings.HasPrefix(path, "http") {
		base := strings.TrimRight(rt.Base, "/")
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		path = base + path
	}
	return c.Invoke(ctx, path, payload)
}

func (c *WeComCLI) ensureMethods(ctx context.Context) error {
	c.mu.Lock()
	if c.discovered && len(c.methods) > 0 {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()
	for _, svc := range []string{"calendar", "doc", "contact"} {
		if err := c.discoverService(ctx, svc); err != nil {
			// Keep going; fallbacks fill gaps.
			_ = err
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seedFallbacksLocked()
	c.discovered = true
	return nil
}

func (c *WeComCLI) seedFallbacksLocked() {
	fallbacks := map[string]string{
		"calendar.schedules.create": "/schedules/create",
		"calendar.schedules.get":    "/schedules/get",
		"calendar.schedules.list":   "/schedules/list",
		"calendar.schedules.search": "/schedules/search",
		"calendar.schedules.update": "/schedules/update",
		"calendar.schedules.cancel": "/schedules/cancel",
		"doc.create":                "/create",
		"doc.contents.get":          "/contents/get",
		"doc.contents.append":       "/contents/append",
		"doc.search":                "/search",
		"doc.members.update":        "/members/update",
		"contact.users.search":      "/users/search",
	}
	for k, p := range fallbacks {
		if _, ok := c.methods[k]; !ok {
			c.methods[k] = wecomCLIRoute{Path: p}
		}
	}
}

func (c *WeComCLI) discoverService(ctx context.Context, name string) error {
	raw, err := c.Invoke(ctx, "/service/discovery", map[string]string{"service": name})
	if err != nil {
		return err
	}
	var svc struct {
		BaseURL   string                      `json:"base_url"`
		Methods   map[string]wecomCLIDiscMeth `json:"methods"`
		Resources map[string]wecomCLIDiscRes  `json:"resources"`
	}
	if err := json.Unmarshal(raw, &svc); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	wecomCLIWalk(c.methods, name, svc.BaseURL, svc.Methods, svc.Resources)
	return nil
}

type wecomCLIDiscMeth struct {
	Path string `json:"path"`
}

type wecomCLIDiscRes struct {
	Methods   map[string]wecomCLIDiscMeth `json:"methods"`
	Resources map[string]wecomCLIDiscRes  `json:"resources"`
}

func wecomCLIWalk(dst map[string]wecomCLIRoute, prefix, base string, methods map[string]wecomCLIDiscMeth, resources map[string]wecomCLIDiscRes) {
	for name, m := range methods {
		path := strings.TrimSpace(m.Path)
		if path == "" {
			continue
		}
		dst[prefix+"."+name] = wecomCLIRoute{Base: base, Path: path}
	}
	for name, res := range resources {
		wecomCLIWalk(dst, prefix+"."+name, base, res.Methods, res.Resources)
	}
}

func (c *WeComCLI) CreateSchedule(ctx context.Context, ev WeComCLISchedule) (string, json.RawMessage, error) {
	body := map[string]any{
		"subject":    strings.TrimSpace(ev.Subject),
		"begin_time": ev.BeginTime,
		"end_time":   ev.EndTime,
	}
	if d := strings.TrimSpace(ev.Description); d != "" {
		body["description"] = d
	}
	if loc := strings.TrimSpace(ev.Location); loc != "" {
		body["location"] = loc
	}
	if ev.WholeDay {
		body["is_all_day"] = true
	}
	if len(ev.Attendees) > 0 {
		atts := make([]map[string]string, 0, len(ev.Attendees))
		seen := map[string]bool{}
		for _, id := range ev.Attendees {
			id = strings.TrimSpace(id)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			atts = append(atts, map[string]string{"userid": id})
		}
		if len(atts) > 0 {
			body["attendees"] = atts
		}
	}
	if ev.RemindSecs > 0 {
		body["reminders"] = map[string]any{
			"is_remind":     true,
			"reminder_time": []int{-ev.RemindSecs},
		}
	}
	raw, err := c.Call(ctx, "calendar.schedules.create", body)
	if err != nil {
		return "", nil, err
	}
	var parsed struct {
		ScheduleID string `json:"schedule_id"`
	}
	_ = json.Unmarshal(raw, &parsed)
	return parsed.ScheduleID, raw, nil
}

func (c *WeComCLI) GetSchedules(ctx context.Context, ids []string) (json.RawMessage, error) {
	return c.Call(ctx, "calendar.schedules.get", map[string]any{"schedule_ids": ids})
}

func (c *WeComCLI) ListSchedules(ctx context.Context, begin, end string) (json.RawMessage, error) {
	body := map[string]any{}
	if begin != "" && end != "" {
		body["begin_time"] = begin
		body["end_time"] = end
	}
	return c.Call(ctx, "calendar.schedules.list", body)
}

func (c *WeComCLI) SearchSchedules(ctx context.Context, keywords []string, begin, end string) (json.RawMessage, error) {
	body := map[string]any{"keywords": keywords}
	if begin != "" && end != "" {
		body["begin_time"] = begin
		body["end_time"] = end
	}
	return c.Call(ctx, "calendar.schedules.search", body)
}

func (c *WeComCLI) CancelSchedule(ctx context.Context, id string) error {
	_, err := c.Call(ctx, "calendar.schedules.cancel", map[string]any{"schedule_id": id})
	return err
}

func (c *WeComCLI) CreateDoc(ctx context.Context, title, content string) (json.RawMessage, error) {
	body := map[string]any{
		"doc_type": "doc",
		"title":    title,
		"name":     title,
	}
	if strings.TrimSpace(content) != "" {
		body["content"] = content
	}
	return c.Call(ctx, "doc.create", body)
}

func (c *WeComCLI) GetDoc(ctx context.Context, docID string) (json.RawMessage, error) {
	return c.Call(ctx, "doc.contents.get", map[string]any{
		"docid":        docID,
		"content_type": "markdown",
	})
}

func (c *WeComCLI) AppendDoc(ctx context.Context, docID, content string) error {
	_, err := c.Call(ctx, "doc.contents.append", map[string]any{
		"docid":   docID,
		"content": content,
	})
	return err
}

func (c *WeComCLI) SearchDocs(ctx context.Context, keywords []string) (json.RawMessage, error) {
	return c.Call(ctx, "doc.search", map[string]any{
		"keywords":     keywords,
		"search_scope": "title_content",
		"limit":        10,
	})
}

func (c *WeComCLI) ShareDoc(ctx context.Context, docID, userid string) error {
	userid = strings.TrimSpace(userid)
	if userid == "" {
		return nil
	}
	_, err := c.Call(ctx, "doc.members.update", map[string]any{
		"docid": docID,
		"add_member_list": map[string]any{
			"items": []map[string]string{{
				"userid":    userid,
				"user_type": "user",
				"user_auth": "edit",
			}},
		},
	})
	return err
}

func (c *WeComCLI) SearchUsers(ctx context.Context, keywords []string) (json.RawMessage, error) {
	return c.Call(ctx, "contact.users.search", map[string]any{"keywords": keywords})
}

// WeComCLIDocID extracts a docid from a raw id or https://doc.weixin.qq.com/doc/… URL.
func WeComCLIDocID(raw string) string {
	return wecomCLIDocIDFromURL(raw)
}

func wecomCLIDocIDFromURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		return raw
	}
	u := raw
	if i := strings.Index(u, "?"); i >= 0 {
		u = u[:i]
	}
	u = strings.TrimRight(u, "/")
	if i := strings.LastIndex(u, "/"); i >= 0 {
		return u[i+1:]
	}
	return raw
}

// WeComCLIPickDoc reads docid / url / name out of a create/search body.
func WeComCLIPickDoc(raw json.RawMessage) (id, url, name string) {
	return wecomCLIPickDocID(raw)
}

func wecomCLIPickDocID(raw json.RawMessage) (id, url, name string) {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return "", "", ""
	}
	id, _ = m["docid"].(string)
	if id == "" {
		id, _ = m["doc_id"].(string)
	}
	url, _ = m["url"].(string)
	name, _ = m["name"].(string)
	if name == "" {
		name, _ = m["title"].(string)
	}
	return strings.TrimSpace(id), strings.TrimSpace(url), strings.TrimSpace(name)
}
