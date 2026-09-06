package xiaohongshu

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseCount(t *testing.T) {
	cases := map[string]int64{
		"":       0,
		"0":      0,
		"1234":   1234,
		"1,234":  1234,
		"1.2万":   12000,
		"10万+":   100000,
		"3亿":     300000000,
		"2.5w":   25000,
		"1.5k":   1500,
		" 88 ":   88,
		"abc":    0,
		"赞":      0,
		"12.34万": 123400,
	}
	for in, want := range cases {
		require.Equal(t, want, ParseCount(in), "输入 %q", in)
	}
}

func feedWith(id, liked, collected string) Feed {
	f := Feed{ID: id, ModelType: modelTypeNote}
	f.NoteCard.InteractInfo.LikedCount = liked
	f.NoteCard.InteractInfo.CollectedCount = collected
	return f
}

func TestSortByEngagement(t *testing.T) {
	feeds := []Feed{
		feedWith("a", "100", "5"),
		feedWith("b", "1.2万", "1"),
		feedWith("c", "100", "50"),
		feedWith("d", "", ""),
		feedWith("e", "100", "50"), // 与 c 完全相同，稳定排序应保持 c 在前
	}

	sortByEngagement(feeds)

	var ids []string
	for _, f := range feeds {
		ids = append(ids, f.ID)
	}
	require.Equal(t, []string{"b", "c", "e", "a", "d"}, ids)
}
