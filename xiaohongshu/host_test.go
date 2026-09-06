package xiaohongshu

import "testing"

func TestWebURL(t *testing.T) {
	t.Cleanup(func() { SetWebHost(DefaultWebHost) })

	if got := WebURL("/explore"); got != "https://www.xiaohongshu.com/explore" {
		t.Fatal(got)
	}
	SetWebHost("") // 空串忽略
	SetWebHost("www.rednote.com")
	if got := WebURL(""); got != "https://www.rednote.com" {
		t.Fatal(got)
	}
	if got := WebURL("/search_result?%s"); got != "https://www.rednote.com/search_result?%s" {
		t.Fatal(got)
	}
}
