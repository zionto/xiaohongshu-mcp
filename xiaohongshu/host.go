package xiaohongshu

import "sync"

// DefaultWebHost 小红书网页版默认主机。
const DefaultWebHost = "www.xiaohongshu.com"

var (
	webHostMu sync.RWMutex
	webHost   = DefaultWebHost
)

// SetWebHost 设置网页版主机名。空串忽略。
//
// 海外账号扫码后会话落在 www.rednote.com 域，xiaohongshu.com 上只有游客态；
// 登录后记录实际落地的主机，之后所有页面都走这个域，cookies 才对得上。
func SetWebHost(host string) {
	if host == "" {
		return
	}
	webHostMu.Lock()
	defer webHostMu.Unlock()
	webHost = host
}

// WebHost 当前网页版主机名。
func WebHost() string {
	webHostMu.RLock()
	defer webHostMu.RUnlock()
	return webHost
}

// WebURL 拼出网页版绝对地址，path 以 / 开头（或为空表示首页）。
func WebURL(path string) string {
	return "https://" + WebHost() + path
}
