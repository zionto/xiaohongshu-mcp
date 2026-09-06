package configs

import (
	"os"
	"strings"

	"github.com/xpzouying/xiaohongshu-mcp/cookies"
)

// WebHostFromEnv 从 XHS_WEB_HOST 环境变量读取网页主机（如 www.rednote.com）。
// 未设返回空。允许带 https:// 前缀和尾部 /，这里顺手去掉。
func WebHostFromEnv() string {
	h := strings.TrimSpace(os.Getenv("XHS_WEB_HOST"))
	h = strings.TrimPrefix(h, "https://")
	h = strings.TrimPrefix(h, "http://")
	return strings.TrimSuffix(h, "/")
}

// ResolveWebHost 决定网页版主机，优先级：
// 环境变量 XHS_WEB_HOST > 会话文件里登录时记录的 > 空（调用方用默认值）。
//
// 背景：海外账号扫码后小红书把会话落在 www.rednote.com，www.xiaohongshu.com 上
// 仍是游客；不跟着会话走，登录了也等于没登录。
func ResolveWebHost(store cookies.Cookier) string {
	if h := WebHostFromEnv(); h != "" {
		return h
	}
	return store.LoadHost()
}
