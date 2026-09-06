// Package ocr 识别图片中的文字。
//
// 后端按可用性选择：
//   - macOS 且装有 Xcode Command Line Tools：用系统 Vision 框架（中文识别效果最好，零依赖）
//   - 其余情况：用 PATH 里的 tesseract（需装 chi_sim 语言包）
package ocr

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
)

//go:embed vision_ocr.swift
var visionScript embed.FS

// tesseractLangs tesseract 识别语言，简体中文 + 英文。
const tesseractLangs = "chi_sim+eng"

// ErrUnavailable 本机没有可用的 OCR 后端。
var ErrUnavailable = errors.New("没有可用的 OCR 后端：macOS 请安装 Xcode Command Line Tools（xcode-select --install），其他系统请安装 tesseract 及 chi_sim 语言包")

type backend int

const (
	backendNone backend = iota
	backendVision
	backendTesseract
)

var (
	detectOnce sync.Once
	detected   backend
)

// detect 选后端，只探测一次。
func detect() backend {
	detectOnce.Do(func() {
		if runtime.GOOS == "darwin" {
			if _, err := exec.LookPath("swift"); err == nil {
				detected = backendVision
				return
			}
		}
		if _, err := exec.LookPath("tesseract"); err == nil {
			detected = backendTesseract
		}
	})
	return detected
}

// Available 返回本机是否有可用后端；不可用时返回带安装提示的错误。
// 调用方应在做昂贵工作（比如打开浏览器）之前先检查。
func Available() error {
	if detect() == backendNone {
		return ErrUnavailable
	}
	return nil
}

// Recognize 识别一组图片，返回与 paths 等长的文本。单张失败只记日志、结果留空；
// 后端本身出错（未安装、语言包缺失）才返回 error。
func Recognize(ctx context.Context, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	switch detect() {
	case backendVision:
		return recognizeVision(ctx, paths)
	case backendTesseract:
		return recognizeTesseract(ctx, paths)
	default:
		return nil, ErrUnavailable
	}
}

// visionScriptPath 把内嵌脚本落到临时目录，只写一次。
var visionScriptPath = sync.OnceValues(func() (string, error) {
	src, err := visionScript.ReadFile("vision_ocr.swift")
	if err != nil {
		return "", err
	}
	path := filepath.Join(os.TempDir(), "xiaohongshu_vision_ocr.swift")
	if err := os.WriteFile(path, src, 0o644); err != nil {
		return "", fmt.Errorf("写入 Vision OCR 脚本失败: %w", err)
	}
	return path, nil
})

// recognizeVision 一次 swift 调用处理全部图片，避免每张都付一次脚本启动开销。
func recognizeVision(ctx context.Context, paths []string) ([]string, error) {
	script, err := visionScriptPath()
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, "swift", append([]string{script}, paths...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("Vision OCR 执行失败: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	var items []struct {
		Text  string `json:"text"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(out, &items); err != nil {
		return nil, fmt.Errorf("解析 Vision OCR 输出失败: %w", err)
	}
	if len(items) != len(paths) {
		return nil, fmt.Errorf("Vision OCR 输出数量不符: 期望 %d，实际 %d", len(paths), len(items))
	}

	texts := make([]string, len(paths))
	for i, it := range items {
		if it.Error != "" {
			logrus.Warnf("OCR 识别 %s 失败: %s", paths[i], it.Error)
			continue
		}
		texts[i] = strings.TrimSpace(it.Text)
	}
	return texts, nil
}

// recognizeTesseract 逐张调用 tesseract。语言包缺失属于环境问题，直接报错而不是静默返回空。
func recognizeTesseract(ctx context.Context, paths []string) ([]string, error) {
	texts := make([]string, len(paths))
	for i, p := range paths {
		cmd := exec.CommandContext(ctx, "tesseract", p, "stdout", "-l", tesseractLangs)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("tesseract 执行失败: %w: %s", err, strings.TrimSpace(stderr.String()))
		}
		texts[i] = cleanTesseract(out)
	}
	return texts, nil
}

// cleanTesseract 去掉 tesseract 在中文字符之间塞的空格，并压掉空行。
func cleanTesseract(out []byte) string {
	var lines []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, stripCJKSpaces(line))
	}
	return strings.Join(lines, "\n")
}

// stripCJKSpaces 删除两个中文字符之间的空格，英文单词间的空格保留。
func stripCJKSpaces(s string) string {
	runes := []rune(s)
	var b strings.Builder
	for i, r := range runes {
		if r == ' ' && i > 0 && i+1 < len(runes) && isCJK(runes[i-1]) && isCJK(runes[i+1]) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isCJK(r rune) bool {
	return r >= 0x4E00 && r <= 0x9FFF
}
