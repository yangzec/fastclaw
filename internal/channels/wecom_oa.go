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

// Official 企业微信自建应用 OpenAPI (OA schedule / later approval).
// Not the AI-bot long-conn. Docs:
//
//	https://developer.work.weixin.qq.com/document/path/91039  (gettoken)
//	https://developer.work.weixin.qq.com/document/path/93703  (schedule/add)
const (
	wecomOADefaultAPI = "https://qyapi.weixin.qq.com"
	wecomOATokenSkew  = 2 * time.Minute
)

// WeComOA is a 自建应用 client. One instance per (corpId, secret).
type WeComOA struct {
	CorpID     string
	CorpSecret string
	AgentID    string
	BaseURL    string
	HTTP       *http.Client

	mu          sync.Mutex
	token       string
	tokenExpiry time.Time
}

// WeComSchedule is the official schedule/add payload (subset we expose).
type WeComSchedule struct {
	Summary     string
	Description string
	Location    string
	StartUnix   int64
	EndUnix     int64
	WholeDay    bool
	Attendees   []string // WeCom userids
	RemindSecs  int      // 0 = no reminder; else seconds before start
	CalID       string
}

type wecomOATokenResp struct {
	ErrCode     int    `json:"errcode"`
	ErrMsg      string `json:"errmsg"`
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

type wecomOAAddResp struct {
	ErrCode    int    `json:"errcode"`
	ErrMsg     string `json:"errmsg"`
	ScheduleID string `json:"schedule_id"`
}

type wecomOAGetResp struct {
	ErrCode      int               `json:"errcode"`
	ErrMsg       string            `json:"errmsg"`
	ScheduleList []json.RawMessage `json:"schedule_list"`
}

// NewWeComOA builds a client. baseURL empty → public qyapi.
func NewWeComOA(corpID, secret, agentID, baseURL string) *WeComOA {
	if baseURL == "" {
		baseURL = wecomOADefaultAPI
	}
	return &WeComOA{
		CorpID:     strings.TrimSpace(corpID),
		CorpSecret: strings.TrimSpace(secret),
		AgentID:    strings.TrimSpace(agentID),
		BaseURL:    strings.TrimRight(baseURL, "/"),
		HTTP:       &http.Client{Timeout: 15 * time.Second},
	}
}

// WeComOAFromChannel reads 自建应用 creds off a wecom ChannelRecord.
func WeComOAFromChannel(ch *store.ChannelRecord) (*WeComOA, error) {
	if ch == nil {
		return nil, fmt.Errorf("wecom oa: channel not bound")
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
	if !ok || strings.TrimSpace(acct.CorpID) == "" || strings.TrimSpace(acct.CorpSecret) == "" {
		return nil, fmt.Errorf("wecom oa: official calendar is not enabled on this WeCom bot — add CorpID + Secret from the 自建应用 on the Channels page")
	}
	return NewWeComOA(acct.CorpID, acct.CorpSecret, acct.CorpAgentID, ""), nil
}

// WeComOAConfigured reports whether the account has 自建应用 creds.
func WeComOAConfigured(acct config.AccountConfig) bool {
	return strings.TrimSpace(acct.CorpID) != "" && strings.TrimSpace(acct.CorpSecret) != ""
}

// WeComValidateOA proves CorpID + Secret by minting an access_token.
func WeComValidateOA(ctx context.Context, corpID, secret string) error {
	_, err := NewWeComOA(corpID, secret, "", "").AccessToken(ctx)
	return err
}

// AccessToken returns a cached corp access_token.
func (c *WeComOA) AccessToken(ctx context.Context) (string, error) {
	if c == nil || c.CorpID == "" || c.CorpSecret == "" {
		return "", fmt.Errorf("wecom oa: corpId and secret required")
	}
	c.mu.Lock()
	if c.token != "" && time.Now().Before(c.tokenExpiry) {
		tok := c.token
		c.mu.Unlock()
		return tok, nil
	}
	c.mu.Unlock()

	q := url.Values{}
	q.Set("corpid", c.CorpID)
	q.Set("corpsecret", c.CorpSecret)
	u := c.BaseURL + "/cgi-bin/gettoken?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	var parsed wecomOATokenResp
	if err := c.doJSON(req, &parsed); err != nil {
		return "", err
	}
	if parsed.ErrCode != 0 || parsed.AccessToken == "" {
		return "", wecomOAErr("gettoken", parsed.ErrCode, parsed.ErrMsg)
	}
	ttl := time.Duration(parsed.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = 2 * time.Hour
	}
	if ttl > wecomOATokenSkew {
		ttl -= wecomOATokenSkew
	}
	c.mu.Lock()
	c.token = parsed.AccessToken
	c.tokenExpiry = time.Now().Add(ttl)
	c.mu.Unlock()
	return parsed.AccessToken, nil
}

// AddSchedule creates an official calendar event. Returns schedule_id.
func (c *WeComOA) AddSchedule(ctx context.Context, ev WeComSchedule) (string, error) {
	if ev.StartUnix <= 0 || ev.EndUnix <= 0 {
		return "", fmt.Errorf("wecom oa: start and end time required")
	}
	if ev.EndUnix <= ev.StartUnix {
		return "", fmt.Errorf("wecom oa: end must be after start")
	}
	tok, err := c.AccessToken(ctx)
	if err != nil {
		return "", err
	}
	body := map[string]any{
		"schedule": c.scheduleBody(ev),
	}
	if id, err := strconv.Atoi(c.AgentID); err == nil && id > 0 {
		body["agentid"] = id
	}
	var parsed wecomOAAddResp
	if err := c.postOA(ctx, "/cgi-bin/oa/schedule/add", tok, body, &parsed); err != nil {
		return "", err
	}
	if parsed.ErrCode != 0 {
		return "", wecomOAErr("schedule/add", parsed.ErrCode, parsed.ErrMsg)
	}
	if parsed.ScheduleID == "" {
		return "", fmt.Errorf("wecom oa schedule/add: empty schedule_id")
	}
	return parsed.ScheduleID, nil
}

// GetSchedules fetches official schedule details by id.
func (c *WeComOA) GetSchedules(ctx context.Context, ids []string) (json.RawMessage, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("wecom oa: schedule id required")
	}
	tok, err := c.AccessToken(ctx)
	if err != nil {
		return nil, err
	}
	var parsed wecomOAGetResp
	if err := c.postOA(ctx, "/cgi-bin/oa/schedule/get", tok, map[string]any{
		"schedule_id_list": ids,
	}, &parsed); err != nil {
		return nil, err
	}
	if parsed.ErrCode != 0 {
		return nil, wecomOAErr("schedule/get", parsed.ErrCode, parsed.ErrMsg)
	}
	raw, err := json.Marshal(parsed.ScheduleList)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func (c *WeComOA) scheduleBody(ev WeComSchedule) map[string]any {
	summary := strings.TrimSpace(ev.Summary)
	if summary == "" {
		summary = "日程"
	}
	sched := map[string]any{
		"start_time": ev.StartUnix,
		"end_time":   ev.EndUnix,
		"summary":    summary,
	}
	if d := strings.TrimSpace(ev.Description); d != "" {
		sched["description"] = d
	}
	if loc := strings.TrimSpace(ev.Location); loc != "" {
		sched["location"] = loc
	}
	if ev.WholeDay {
		sched["is_whole_day"] = 1
	}
	if cal := strings.TrimSpace(ev.CalID); cal != "" {
		sched["cal_id"] = cal
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
			sched["attendees"] = atts
		}
	}
	if ev.RemindSecs > 0 {
		sched["reminders"] = map[string]any{
			"is_remind":                1,
			"remind_before_event_secs": ev.RemindSecs,
		}
	}
	return sched
}

func (c *WeComOA) postOA(ctx context.Context, path, token string, body any, dest any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	u := c.BaseURL + path + "?access_token=" + url.QueryEscape(token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.doJSON(req, dest)
}

func (c *WeComOA) doJSON(req *http.Request, dest any) error {
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("wecom oa: %w", err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("wecom oa: read body: %w", err)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("wecom oa: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	if err := json.Unmarshal(payload, dest); err != nil {
		return fmt.Errorf("wecom oa: decode: %w", err)
	}
	return nil
}

func wecomOAErr(op string, code int, msg string) error {
	msg = strings.TrimSpace(msg)
	switch code {
	case 48002:
		return fmt.Errorf("wecom oa %s: 48002 无日程权限。管理后台「协作 → 日程 → 可调用接口的应用」勾选该自建应用；可见范围要覆盖被邀请的人；若仍失败，把 FastClaw 出口 IP 加到应用的企业可信 IP。权限开通前请改用 create_cron_job 做本地到点提醒（不会出现在企微日历）", op)
	case 60020:
		return fmt.Errorf("wecom oa %s: 60020 企业可信 IP 未放行。把当前服务器公网 IP 加到该自建应用的可信 IP 列表", op)
	default:
		if msg == "" {
			return fmt.Errorf("wecom oa %s: %d", op, code)
		}
		return fmt.Errorf("wecom oa %s: %d %s", op, code, msg)
	}
}
