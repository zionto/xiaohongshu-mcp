package cookies

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestHostRoundTripKeepsCookiesAndSeed(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cookies.json")
	c := NewLoadCookie(p)

	if err := c.SaveSeed(42); err != nil {
		t.Fatal(err)
	}
	if err := c.SaveCookies([]byte(`[{"name":"a","value":"b"}]`)); err != nil {
		t.Fatal(err)
	}
	if got := c.LoadHost(); got != "" {
		t.Fatalf("host before save = %q, want empty", got)
	}
	if err := c.SaveHost("www.rednote.com"); err != nil {
		t.Fatal(err)
	}
	if got := c.LoadHost(); got != "www.rednote.com" {
		t.Fatalf("host = %q", got)
	}
	if got := c.LoadSeed(); got != 42 {
		t.Fatalf("seed lost after SaveHost: %d", got)
	}
	cks, err := c.LoadCookies()
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]string
	if err := json.Unmarshal(cks, &got); err != nil || len(got) != 1 || got[0]["value"] != "b" {
		t.Fatalf("cookies lost after SaveHost: %s %v", cks, err)
	}

	// SaveCookies / SaveSeed 也不能把 host 冲掉
	if err := c.SaveCookies([]byte(`[]`)); err != nil {
		t.Fatal(err)
	}
	if err := c.SaveSeed(7); err != nil {
		t.Fatal(err)
	}
	if got := c.LoadHost(); got != "www.rednote.com" {
		t.Fatalf("host lost after SaveCookies/SaveSeed: %q", got)
	}
}

func TestLoadHostLegacyArrayFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cookies.json")
	if err := os.WriteFile(p, []byte(`[{"name":"a","value":"b"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := NewLoadCookie(p).LoadHost(); got != "" {
		t.Fatalf("legacy file host = %q, want empty", got)
	}
}
