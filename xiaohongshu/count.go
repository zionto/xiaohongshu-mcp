package xiaohongshu

import (
	"sort"
	"strconv"
	"strings"
)

// ParseCount 把站点展示用的计数文本转成数字，如 "1234"、"1.2万"、"3亿"、"10万+"。
// 解析不了返回 0，调用方只拿它做排序，不需要区分「0」和「未知」。
func ParseCount(s string) int64 {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "+")
	s = strings.ReplaceAll(s, ",", "")
	if s == "" {
		return 0
	}

	multiplier := 1.0
	switch {
	case strings.HasSuffix(s, "亿"):
		multiplier = 1e8
		s = strings.TrimSuffix(s, "亿")
	case strings.HasSuffix(s, "万"):
		multiplier = 1e4
		s = strings.TrimSuffix(s, "万")
	case strings.HasSuffix(s, "w"), strings.HasSuffix(s, "W"):
		multiplier = 1e4
		s = s[:len(s)-1]
	case strings.HasSuffix(s, "k"), strings.HasSuffix(s, "K"):
		multiplier = 1e3
		s = s[:len(s)-1]
	}

	n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return int64(n * multiplier)
}

// sortByEngagement 按点赞数倒序，点赞相同再按收藏数倒序；稳定排序，其余保持站点原序。
func sortByEngagement(feeds []Feed) {
	sort.SliceStable(feeds, func(i, j int) bool {
		a, b := feeds[i].NoteCard.InteractInfo, feeds[j].NoteCard.InteractInfo
		la, lb := ParseCount(a.LikedCount), ParseCount(b.LikedCount)
		if la != lb {
			return la > lb
		}
		return ParseCount(a.CollectedCount) > ParseCount(b.CollectedCount)
	})
}
