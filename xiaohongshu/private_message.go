package xiaohongshu

import (
	"context"
	"fmt"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/sirupsen/logrus"
	"github.com/xpzouying/xiaohongshu-mcp/humanize"
)

// PrivateMessageAction 私信动作
type PrivateMessageAction struct {
	page *rod.Page
}

// NewPrivateMessageAction 创建私信动作
func NewPrivateMessageAction(page *rod.Page) *PrivateMessageAction {
	return &PrivateMessageAction{page: page}
}

// SendPrivateMessage 从用户主页点「发消息」打开聊天面板并发送一条私信。
// 流程与评论一致：导航 -> humanize 延迟 -> 点击 -> 逐字输入 -> 发送 -> 就地校验。
func (a *PrivateMessageAction) SendPrivateMessage(ctx context.Context, userID, xsecToken, content string) error {
	// 不使用 Context(ctx)，避免继承外部 context 的超时
	page := a.page.Timeout(60 * time.Second)

	url := makeUserProfileURL(userID, xsecToken, TabNotes)
	logrus.Infof("打开用户主页发私信: %s", url)

	page.MustNavigate(url)
	page.MustWaitDOMStable()
	humanize.Delay(ctx, humanize.AfterNavigate)

	if err := checkPageAccessible(page); err != nil {
		return err
	}

	// 主页头部的「发消息」按钮
	imBtn, err := page.Element("button.xhs-user-im-btn")
	if err != nil {
		return fmt.Errorf("未找到「发消息」按钮，对方可能关闭了私信或网页端不可访问: %w", err)
	}
	if err := humanize.Click(imBtn); err != nil {
		return fmt.Errorf("无法点击「发消息」按钮: %w", err)
	}
	humanize.Delay(ctx, humanize.AfterClick)

	// 聊天面板可能在新标签页打开，优先切过去
	page = chatPage(page)

	inputEl, err := waitMessageInput(page, 10*time.Second)
	if err != nil {
		return err
	}
	if err := humanize.Click(inputEl); err != nil {
		return fmt.Errorf("无法点击私信输入框: %w", err)
	}
	humanize.Delay(ctx, humanize.AfterClick)

	if err := humanize.Type(ctx, inputEl, content); err != nil {
		return fmt.Errorf("无法输入私信内容: %w", err)
	}
	humanize.Delay(ctx, humanize.AfterType)

	// 优先点「发送」按钮，找不到则回车发送
	if sendBtn, err := page.Timeout(3*time.Second).ElementR("button", `^\s*发送\s*$`); err == nil {
		if err := humanize.Click(sendBtn); err != nil {
			return fmt.Errorf("无法点击发送按钮: %w", err)
		}
	} else if err := page.Keyboard.Press(input.Enter); err != nil {
		return fmt.Errorf("回车发送失败: %w", err)
	}
	humanize.Delay(ctx, humanize.AfterClick)

	// 就地校验：输入框已清空且消息文本出现在页面上，否则判定失败，避免假成功
	if !waitMessageSent(page, inputEl, content, 4*time.Second) {
		logrus.Warnf("私信发送后未在会话中出现，判定未成功: user=%s", userID)
		return fmt.Errorf("私信未确认成功：发送后未在会话中出现（可能账号被限制或对方不接收私信），user: %s", userID)
	}

	logrus.Infof("私信发送成功并已确认: user=%s", userID)
	return nil
}

// chatPage 若点击后弹出了新标签页，返回最新的那个，否则返回原页面
func chatPage(page *rod.Page) *rod.Page {
	pages, err := page.Browser().Pages()
	if err != nil || len(pages) <= 1 {
		return page
	}
	latest := pages[len(pages)-1]
	if latest.TargetID == page.TargetID {
		return page
	}
	logrus.Info("聊天面板在新标签页打开，切换过去")
	return latest.Timeout(60 * time.Second)
}

// messageInputSelectors 聊天面板输入框候选选择器，从具体到通用
var messageInputSelectors = []string{
	`div[class*="im"] textarea`,
	`div[class*="chat"] textarea`,
	`div[class*="chat"] [contenteditable="true"]`,
	`textarea`,
	`[contenteditable="true"]`,
}

// waitMessageInput 轮询查找可见的私信输入框
func waitMessageInput(page *rod.Page, timeout time.Duration) (*rod.Element, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, sel := range messageInputSelectors {
			elems, err := page.Timeout(2 * time.Second).Elements(sel)
			if err != nil {
				continue
			}
			for _, el := range elems {
				if visible, _ := el.Visible(); visible {
					return el, nil
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return nil, fmt.Errorf("未找到私信输入框，聊天面板可能未打开")
}

// messageSent 只读检查：输入框已清空，且消息文本出现在页面可见文本中
func messageSent(page *rod.Page, inputEl *rod.Element, content string) bool {
	res, err := inputEl.Eval(`() => ((this.value ?? this.innerText) || "").trim() === ""`)
	if err != nil || !res.Value.Bool() {
		return false
	}
	res, err = page.Eval(`(txt) => document.body.innerText.includes(txt)`, content)
	if err != nil {
		return false
	}
	return res.Value.Bool()
}

func waitMessageSent(page *rod.Page, inputEl *rod.Element, content string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if messageSent(page, inputEl, content) {
			return true
		}
		time.Sleep(300 * time.Millisecond)
	}
	return false
}
