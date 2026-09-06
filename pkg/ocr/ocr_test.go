package ocr

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStripCJKSpaces(t *testing.T) {
	require.Equal(t, "小红书图片", stripCJKSpaces("小 红 书 图 片"))
	require.Equal(t, "OCR TEST 2026", stripCJKSpaces("OCR TEST 2026"))
	require.Equal(t, "中文 OCR 测试", stripCJKSpaces("中文 OCR 测试"))
}

func TestCleanTesseract(t *testing.T) {
	out := []byte("小 红 书\n\n  OCR TEST  \n\n")
	require.Equal(t, "小红书\nOCR TEST", cleanTesseract(out))
}

// TestRecognize 走真实后端，本机没有可用后端时跳过。
func TestRecognize(t *testing.T) {
	if err := Available(); err != nil {
		t.Skip(err)
	}

	texts, err := Recognize(context.Background(), []string{"testdata/sample.png"})
	require.NoError(t, err)
	require.Len(t, texts, 1)
	require.Contains(t, texts[0], "文字识别")
	require.Contains(t, texts[0], "OCR TEST 2026")
}

func TestRecognizeEmpty(t *testing.T) {
	texts, err := Recognize(context.Background(), nil)
	require.NoError(t, err)
	require.Empty(t, texts)
}
