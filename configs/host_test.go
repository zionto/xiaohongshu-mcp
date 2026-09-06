package configs

import (
	"path/filepath"
	"testing"

	"github.com/xpzouying/xiaohongshu-mcp/cookies"
)

func TestResolveWebHostPrefersEnvThenFile(t *testing.T) {
	store := cookies.NewLoadCookie(filepath.Join(t.TempDir(), "cookies.json"))

	t.Setenv("XHS_WEB_HOST", "")
	if got := ResolveWebHost(store); got != "" {
		t.Fatalf("empty store, got %q", got)
	}
	if err := store.SaveHost("www.rednote.com"); err != nil {
		t.Fatal(err)
	}
	if got := ResolveWebHost(store); got != "www.rednote.com" {
		t.Fatalf("from file: %q", got)
	}
	t.Setenv("XHS_WEB_HOST", "https://www.xiaohongshu.com/")
	if got := ResolveWebHost(store); got != "www.xiaohongshu.com" {
		t.Fatalf("env should win and be normalized: %q", got)
	}
}
