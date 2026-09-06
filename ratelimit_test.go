package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fastLimiter 间隔设 0，测试不用等
func fastLimiter(t *testing.T) *RateLimiter {
	t.Helper()
	t.Setenv("XHS_READ_MIN_GAP", "0")
	t.Setenv("XHS_READ_JITTER", "0")
	return NewRateLimiter(filepath.Join(t.TempDir(), "state.json"))
}

func TestRateLimiterExemptNeverCounts(t *testing.T) {
	r := fastLimiter(t)
	for i := 0; i < 5; i++ {
		done, reason := r.Acquire(context.Background(), classExempt)
		require.Empty(t, reason)
		done("")
	}
	assert.Equal(t, 0, r.Summary()["reads_last_hour"])
}

func TestRateLimiterHourlyReadCap(t *testing.T) {
	t.Setenv("XHS_READ_PER_HOUR", "2")
	r := fastLimiter(t)
	for i := 0; i < 2; i++ {
		done, reason := r.Acquire(context.Background(), classRead)
		require.Empty(t, reason)
		done("ok")
	}
	_, reason := r.Acquire(context.Background(), classRead)
	assert.Contains(t, reason, "hourly read cap")
}

func TestRateLimiterWriteGapAndDisable(t *testing.T) {
	r := fastLimiter(t)
	done, reason := r.Acquire(context.Background(), classComment)
	require.Empty(t, reason, "写操作默认允许")
	done("ok")
	_, reason = r.Acquire(context.Background(), classComment)
	assert.Contains(t, reason, "comment gap not elapsed")

	t.Setenv("XHS_ALLOW_WRITE", "0")
	r = fastLimiter(t)
	_, reason = r.Acquire(context.Background(), classPublish)
	assert.Contains(t, reason, "disabled")
}

func TestRateLimiterRiskMarkerTripsCooldown(t *testing.T) {
	r := fastLimiter(t)
	done, reason := r.Acquire(context.Background(), classRead)
	require.Empty(t, reason)
	done(`{"error":"操作频繁，请稍后再试"}`)
	_, reason = r.Acquire(context.Background(), classRead)
	assert.Contains(t, reason, "cooldown active")
	assert.Greater(t, r.Summary()["cooldown_remaining_sec"], 0)
}

func TestRateLimiterStatePersists(t *testing.T) {
	t.Setenv("XHS_READ_PER_DAY", "1")
	path := filepath.Join(t.TempDir(), "state.json")
	t.Setenv("XHS_READ_MIN_GAP", "0")
	t.Setenv("XHS_READ_JITTER", "0")

	r := NewRateLimiter(path)
	done, reason := r.Acquire(context.Background(), classRead)
	require.Empty(t, reason)
	done("ok")

	again := NewRateLimiter(path)
	_, reason = again.Acquire(context.Background(), classRead)
	assert.Contains(t, reason, "daily read cap")
}

func TestRateLimitAPIMiddlewareReturns429(t *testing.T) {
	t.Setenv("XHS_READ_PER_HOUR", "0")
	r := fastLimiter(t)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	api := router.Group("/api/v1")
	api.Use(r.apiMiddleware())
	api.GET("/feeds/list", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	api.GET("/login/status", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/feeds/list", nil))
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Contains(t, rec.Body.String(), "RATE_LIMITED")

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/login/status", nil))
	assert.Equal(t, http.StatusNoContent, rec.Code, "登录状态不受限")
}

// TestRateLimitMCPToolCallDenied 通过真实路由验证 tools/call 被拒时返回 isError 结果，
// 而不是 JSON-RPC 错误。用上限 0 保证在触碰浏览器之前就被拒。
func TestRateLimitMCPToolCallDenied(t *testing.T) {
	t.Setenv("XHS_READ_PER_HOUR", "0")
	t.Setenv("XHS_RATELIMIT_STATE", filepath.Join(t.TempDir(), "state.json"))
	router := setupRoutes(NewAppServer(NewXiaohongshuService(), ""))
	server := httptest.NewServer(router)
	defer server.Close()

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_feeds","arguments":{}}}`
	req, err := http.NewRequest(http.MethodPost, server.URL+"/mcp", strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Error  *json.RawMessage `json:"error"`
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Nil(t, out.Error)
	assert.True(t, out.Result.IsError)
	require.NotEmpty(t, out.Result.Content)
	assert.Contains(t, out.Result.Content[0].Text, "hourly read cap")

	// 状态接口
	sresp, err := http.Get(server.URL + "/ratelimit/status")
	require.NoError(t, err)
	defer sresp.Body.Close()
	var status map[string]any
	require.NoError(t, json.NewDecoder(sresp.Body).Decode(&status))
	assert.Equal(t, true, status["allow_write"])
}
