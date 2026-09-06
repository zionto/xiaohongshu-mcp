package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sirupsen/logrus"
	"github.com/xpzouying/xiaohongshu-mcp/cookies"
)

// 账号级频率限制。每个 MCP 工具调用 / HTTP API 请求都会在登录态浏览器里
// 操作真实页面，密集调用等同于爬虫，会触发风控甚至封号。这里在
// humanize 的页面级延迟之上，再加一层请求级限制：
//
//   - 同一时刻只执行一个动作，动作之间留随机间隔
//   - 读操作按小时 / 天计数封顶
//   - 写操作（发布、评论、点赞收藏）各有更长的间隔和更小的日上限
//   - 响应里出现风控特征（验证码、操作频繁等）后进入冷却期，期间全部拒绝
//
// 计数写入 JSON 状态文件，重启不清零。所有阈值可用 XHS_* 环境变量覆盖。

// actionClass 动作类别
type actionClass string

const (
	classExempt  actionClass = "exempt" // 登录、二维码、删 cookies：不碰小红书内容页
	classRead    actionClass = "read"
	classPublish actionClass = "publish"
	classComment actionClass = "comment"
	classReact   actionClass = "react"
)

// isWrite 是否为写类动作
func (c actionClass) isWrite() bool {
	return c == classPublish || c == classComment || c == classReact
}

// toolClasses MCP 工具名 -> 类别，未登记的工具按 read 处理
var toolClasses = map[string]actionClass{
	"check_login_status":    classExempt,
	"get_login_qrcode":      classExempt,
	"delete_cookies":        classExempt,
	"list_feeds":            classRead,
	"search_feeds":          classRead,
	"get_feed_detail":       classRead,
	"user_profile":          classRead,
	"get_my_profile":        classRead,
	"get_unread_count":      classRead,
	"list_notifications":    classRead,
	"publish_content":       classPublish,
	"publish_with_video":    classPublish,
	"post_comment_to_feed":  classComment,
	"reply_comment_in_feed": classComment,
	"reply_notification":    classComment,
	"like_feed":             classReact,
	"favorite_feed":         classReact,
	"like_notification":     classReact,
}

// apiClasses "METHOD path" -> 类别，未登记的 /api/v1 路由按 read 处理
var apiClasses = map[string]actionClass{
	"GET /api/v1/login/status":         classExempt,
	"GET /api/v1/login/qrcode":         classExempt,
	"DELETE /api/v1/login/cookies":     classExempt,
	"GET /api/v1/feeds/list":           classRead,
	"GET /api/v1/feeds/search":         classRead,
	"POST /api/v1/feeds/search":        classRead,
	"POST /api/v1/feeds/detail":        classRead,
	"POST /api/v1/user/profile":        classRead,
	"GET /api/v1/user/me":              classRead,
	"GET /api/v1/notifications/unread": classRead,
	"GET /api/v1/notifications/list":   classRead,
	"POST /api/v1/notifications/list":  classRead,
	"POST /api/v1/publish":             classPublish,
	"POST /api/v1/publish_video":       classPublish,
	"POST /api/v1/feeds/comment":       classComment,
	"POST /api/v1/feeds/comment/reply": classComment,
	"POST /api/v1/notifications/reply": classComment,
	"POST /api/v1/feeds/like":          classReact,
	"POST /api/v1/feeds/favorite":      classReact,
	"POST /api/v1/notifications/like":  classReact,
}

// riskMarkers 响应中出现即视为风控信号（小写匹配）
var riskMarkers = []string{
	"验证码", "风控", "操作频繁", "操作太频繁", "请稍后再试", "账号异常",
	"安全验证", "滑块", "captcha", "too many requests", "访问频繁",
}

// rateLimits 阈值，秒 / 次数
type rateLimits struct {
	AllowWrite    bool    `json:"allow_write"`
	ReadMinGap    float64 `json:"read_min_gap"`
	ReadJitter    float64 `json:"read_jitter"`
	ReadPerHour   int     `json:"read_per_hour"`
	ReadPerDay    int     `json:"read_per_day"`
	PublishMinGap float64 `json:"publish_min_gap"`
	PublishPerDay int     `json:"publish_per_day"`
	CommentMinGap float64 `json:"comment_min_gap"`
	CommentPerDay int     `json:"comment_per_day"`
	ReactMinGap   float64 `json:"react_min_gap"`
	ReactPerDay   int     `json:"react_per_day"`
	CooldownSec   float64 `json:"cooldown_sec"`
}

// loadRateLimits 默认值叠加 XHS_* 环境变量
func loadRateLimits() rateLimits {
	l := rateLimits{
		AllowWrite:    true,
		ReadMinGap:    6,
		ReadJitter:    6,
		ReadPerHour:   60,
		ReadPerDay:    400,
		PublishMinGap: 1800,
		PublishPerDay: 3,
		CommentMinGap: 90,
		CommentPerDay: 20,
		ReactMinGap:   20,
		ReactPerDay:   30,
		CooldownSec:   1800,
	}
	envFloat := func(name string, dst *float64) {
		if v, err := strconv.ParseFloat(os.Getenv("XHS_"+name), 64); err == nil {
			*dst = v
		}
	}
	envInt := func(name string, dst *int) {
		if v, err := strconv.Atoi(os.Getenv("XHS_" + name)); err == nil {
			*dst = v
		}
	}
	if v := os.Getenv("XHS_ALLOW_WRITE"); v != "" {
		l.AllowWrite = v == "1"
	}
	envFloat("READ_MIN_GAP", &l.ReadMinGap)
	envFloat("READ_JITTER", &l.ReadJitter)
	envInt("READ_PER_HOUR", &l.ReadPerHour)
	envInt("READ_PER_DAY", &l.ReadPerDay)
	envFloat("PUBLISH_MIN_GAP", &l.PublishMinGap)
	envInt("PUBLISH_PER_DAY", &l.PublishPerDay)
	envFloat("COMMENT_MIN_GAP", &l.CommentMinGap)
	envInt("COMMENT_PER_DAY", &l.CommentPerDay)
	envFloat("REACT_MIN_GAP", &l.ReactMinGap)
	envInt("REACT_PER_DAY", &l.ReactPerDay)
	envFloat("COOLDOWN_SEC", &l.CooldownSec)
	return l
}

// writeLimits 写类动作各自的间隔和日上限
func (l rateLimits) writeLimits(cls actionClass) (gap float64, perDay int) {
	switch cls {
	case classPublish:
		return l.PublishMinGap, l.PublishPerDay
	case classComment:
		return l.CommentMinGap, l.CommentPerDay
	default:
		return l.ReactMinGap, l.ReactPerDay
	}
}

// rateState 持久化状态，字段名与旧版 guard.py 的状态文件兼容
type rateState struct {
	Events        map[string][]float64 `json:"events"`
	LastAction    float64              `json:"last_action"`
	CooldownUntil float64              `json:"cooldown_until"`
}

// RateLimiter 串行化动作并执行间隔、上限、冷却
type RateLimiter struct {
	limits rateLimits
	path   string

	mu    sync.Mutex // 保护 state
	slot  sync.Mutex // 同一时刻只允许一个动作
	state rateState
}

// rateLimitStatePath 状态文件：XHS_RATELIMIT_STATE 优先，否则放在 cookies 同目录
func rateLimitStatePath() string {
	if p := os.Getenv("XHS_RATELIMIT_STATE"); p != "" {
		return p
	}
	return filepath.Join(filepath.Dir(cookies.GetCookiesFilePath()), "ratelimit-state.json")
}

// NewRateLimiter 读取环境变量和状态文件
func NewRateLimiter(statePath string) *RateLimiter {
	r := &RateLimiter{
		limits: loadRateLimits(),
		path:   statePath,
		state:  rateState{Events: map[string][]float64{}},
	}
	if data, err := os.ReadFile(statePath); err == nil {
		if err := json.Unmarshal(data, &r.state); err != nil {
			logrus.Warnf("rate limit state file %s unreadable, starting fresh: %v", statePath, err)
		}
		if r.state.Events == nil {
			r.state.Events = map[string][]float64{}
		}
	}
	logrus.Infof("rate limit enabled, writes %s, state file %s",
		map[bool]string{true: "allowed", false: "disabled"}[r.limits.AllowWrite], statePath)
	return r
}

func nowSec() float64 { return float64(time.Now().UnixNano()) / 1e9 }

// recent 返回 window 秒内的动作时间戳，并顺手清掉过期的。调用方须持有 mu。
func (r *RateLimiter) recent(cls actionClass, window float64) []float64 {
	now := nowSec()
	kept := r.state.Events[string(cls)][:0]
	for _, t := range r.state.Events[string(cls)] {
		if now-t < window {
			kept = append(kept, t)
		}
	}
	r.state.Events[string(cls)] = kept
	return kept
}

// check 返回拒绝原因，空串表示放行
func (r *RateLimiter) check(cls actionClass) string {
	now := nowSec()
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.state.CooldownUntil > now {
		mins := int((r.state.CooldownUntil-now)/60) + 1
		return fmt.Sprintf("cooldown active for another ~%d min: an earlier response looked like Xiaohongshu risk control. Stop and wait.", mins)
	}
	if cls.isWrite() && !r.limits.AllowWrite {
		return fmt.Sprintf("'%s' actions are disabled (XHS_ALLOW_WRITE=0).", cls)
	}
	if cls == classRead {
		if len(r.recent(cls, 3600)) >= r.limits.ReadPerHour {
			return "hourly read cap reached; wait before more searches or detail reads."
		}
		if len(r.recent(cls, 86400)) >= r.limits.ReadPerDay {
			return "daily read cap reached; no more Xiaohongshu reads today."
		}
		return ""
	}
	gap, perDay := r.limits.writeLimits(cls)
	events := r.recent(cls, 86400)
	if len(events) >= perDay {
		return fmt.Sprintf("daily %s cap reached.", cls)
	}
	if n := len(events); n > 0 && now-events[n-1] < gap {
		return fmt.Sprintf("%s gap not elapsed; wait ~%ds.", cls, int(gap-(now-events[n-1]))+1)
	}
	return ""
}

// Acquire 申请执行一个动作。返回 reason 非空表示拒绝；否则动作完成后
// 必须调用 done 并传入响应文本，用于计数和风控特征扫描。
// exempt 类直接放行且不计数。
func (r *RateLimiter) Acquire(ctx context.Context, cls actionClass) (done func(respText string), reason string) {
	if cls == classExempt {
		return func(string) {}, ""
	}
	if reason = r.check(cls); reason != "" {
		return nil, reason
	}

	// 占住执行槽，等够间隔（读间隔 + 随机抖动，对所有类别生效）
	r.slot.Lock()
	r.mu.Lock()
	last := r.state.LastAction
	r.mu.Unlock()
	gap := r.limits.ReadMinGap + rand.Float64()*r.limits.ReadJitter
	if wait := time.Duration((last + gap - nowSec()) * float64(time.Second)); wait > 0 {
		logrus.Infof("rate limit: %s pacing %.1fs", cls, wait.Seconds())
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			r.slot.Unlock()
			return nil, "request cancelled while waiting for the rate limit gap."
		}
	}
	// 等待期间状态可能已变（其他请求触发冷却），再查一次
	if reason = r.check(cls); reason != "" {
		r.slot.Unlock()
		return nil, reason
	}

	return func(respText string) {
		defer r.slot.Unlock()
		r.mu.Lock()
		defer r.mu.Unlock()
		now := nowSec()
		r.state.LastAction = now
		r.state.Events[string(cls)] = append(r.state.Events[string(cls)], now)
		if looksLikeRiskControl(respText) {
			r.state.CooldownUntil = now + r.limits.CooldownSec
			logrus.Warnf("RISK CONTROL MARKER SEEN - cooldown tripped, actions blocked for %d min",
				int(r.limits.CooldownSec/60))
		}
		if err := r.save(); err != nil {
			logrus.Warnf("rate limit state save failed: %v", err)
		}
	}, ""
}

// looksLikeRiskControl 响应文本是否含风控特征
func looksLikeRiskControl(text string) bool {
	lower := strings.ToLower(text)
	for _, m := range riskMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// save 原子写状态文件。调用方须持有 mu。
func (r *RateLimiter) save() error {
	data, err := json.Marshal(r.state)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0o700); err != nil {
		return err
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}

// Summary 供状态接口展示
func (r *RateLimiter) Summary() map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	cooldown := int(r.state.CooldownUntil - nowSec())
	if cooldown < 0 {
		cooldown = 0
	}
	return map[string]any{
		"allow_write":            r.limits.AllowWrite,
		"reads_last_hour":        len(r.recent(classRead, 3600)),
		"reads_last_day":         len(r.recent(classRead, 86400)),
		"publish_last_day":       len(r.recent(classPublish, 86400)),
		"comment_last_day":       len(r.recent(classComment, 86400)),
		"react_last_day":         len(r.recent(classReact, 86400)),
		"cooldown_remaining_sec": cooldown,
		"limits":                 r.limits,
	}
}

// mcpMiddleware MCP 层：只拦 tools/call，拒绝时返回 isError 的工具结果，
// 让调用方模型看到原因而不是传输错误后盲目重试。
func (r *RateLimiter) mcpMiddleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		call, ok := req.(*mcp.CallToolRequest)
		if method != "tools/call" || !ok || call.Params == nil {
			return next(ctx, method, req)
		}
		cls, known := toolClasses[call.Params.Name]
		if !known {
			cls = classRead
		}
		done, reason := r.Acquire(ctx, cls)
		if reason != "" {
			logrus.Warnf("rate limit DENY tool %s: %s", call.Params.Name, reason)
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: "rate limit refused this call: " + reason}},
			}, nil
		}
		result, err := next(ctx, method, req)
		var text strings.Builder
		if err != nil {
			text.WriteString(err.Error())
		}
		if res, ok := result.(*mcp.CallToolResult); ok {
			for _, c := range res.Content {
				if t, ok := c.(*mcp.TextContent); ok {
					text.WriteString(t.Text)
				}
			}
		}
		done(text.String())
		return result, err
	}
}

// bodyRecorder 抓一份响应体用于风控特征扫描
type bodyRecorder struct {
	gin.ResponseWriter
	body bytes.Buffer
}

func (w *bodyRecorder) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func (w *bodyRecorder) WriteString(s string) (int, error) {
	w.body.WriteString(s)
	return w.ResponseWriter.WriteString(s)
}

// apiMiddleware HTTP API 层：拒绝时返回 429
func (r *RateLimiter) apiMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		cls, known := apiClasses[c.Request.Method+" "+c.Request.URL.Path]
		if !known {
			cls = classRead
		}
		done, reason := r.Acquire(c.Request.Context(), cls)
		if reason != "" {
			respondError(c, http.StatusTooManyRequests, "RATE_LIMITED", reason, nil)
			c.Abort()
			return
		}
		rec := &bodyRecorder{ResponseWriter: c.Writer}
		c.Writer = rec
		c.Next()
		done(rec.body.String())
	}
}

// statusHandler [GET /ratelimit/status]
func (r *RateLimiter) statusHandler(c *gin.Context) {
	c.JSON(http.StatusOK, r.Summary())
}
