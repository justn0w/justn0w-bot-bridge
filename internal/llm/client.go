// Package llm 封装 Anthropic Messages 协议，为答疑场景提供单轮问答能力。
//
// 包名刻意不叫 claude：任何兼容 Anthropic Messages 协议的服务都能直接复用本包，
// 只要把 Options.BaseURL 指向对应入口即可。当前默认接入 DeepSeek
// （https://api.deepseek.com/anthropic），SDK 会自动拼接 /v1/messages。
package llm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// textBlockType 是响应中文本块的类型标识，thinking / tool_use 等块需要跳过
const textBlockType = "text"

// 防御性默认值：调用方传入零值 Options 时兜底。
// 应用侧的真源在 internal/config（config.yaml 的 llm 段），这里只是避免
// Timeout 为 0 导致 context 创建即超时、MaxTokens 为 0 导致 API 返回 400。
const (
	defaultMaxTokens = int64(2000)
	defaultTimeout   = 90 * time.Second
)

// systemPrompt 约束答疑场景下的回答风格与长度
const systemPrompt = `你是团队内部的答疑助手，服务于飞书群里的值班答疑机器人。
回答要求：
- 先给结论，再补充必要说明，不要寒暄。
- 使用中文，尽量控制在 300 字以内。
- 不确定时明确说明「无法确定」，不要编造。
- 涉及代码时给出最小可运行的示例。`

// Options 是构造 Client 所需的配置。
// 独立于 internal/config，避免包之间产生反向依赖。
type Options struct {
	APIKey string
	// BaseURL 指向兼容 Anthropic Messages 协议的服务入口。
	// 留空则使用 Anthropic 官方地址；接入 DeepSeek 时填
	// https://api.deepseek.com/anthropic（SDK 会自动补 /v1/messages）。
	BaseURL   string
	Model     string
	MaxTokens int64
	Timeout   time.Duration
}

// Answer 是一次答疑的结构化结果
type Answer struct {
	Text      string // 答案正文
	ElapsedMS int64  // 调用耗时（毫秒），用于观测与超时分析
}

// Client 是对 Anthropic Messages 协议的薄封装
type Client struct {
	api       anthropic.Client
	model     anthropic.Model
	maxTokens int64
	timeout   time.Duration
}

// NewClient 创建答疑模型客户端
func NewClient(opts Options) *Client {
	if opts.MaxTokens <= 0 {
		opts.MaxTokens = defaultMaxTokens
	}
	if opts.Timeout <= 0 {
		opts.Timeout = defaultTimeout
	}

	requestOpts := []option.RequestOption{option.WithAPIKey(opts.APIKey)}
	if opts.BaseURL != "" {
		requestOpts = append(requestOpts, option.WithBaseURL(opts.BaseURL))
	}

	return &Client{
		api:       anthropic.NewClient(requestOpts...),
		model:     opts.Model,
		maxTokens: opts.MaxTokens,
		timeout:   opts.Timeout,
	}
}

// Ask 提交单轮提问并返回答案。
// 超时由 Client 自身控制：即使传入的 ctx 没有 deadline，调用也不会无限挂起。
func (c *Client) Ask(ctx context.Context, question string) (*Answer, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	start := time.Now()
	msg, err := c.api.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     c.model,
		MaxTokens: c.maxTokens,
		System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(question)),
		},
	})
	if err != nil {
		// 带上模型名：接错 BaseURL 或写错模型标识时，仅凭错误信息难以定位
		return nil, fmt.Errorf("调用模型 %s 失败: %w", c.model, err)
	}

	return &Answer{
		Text:      extractText(msg),
		ElapsedMS: time.Since(start).Milliseconds(),
	}, nil
}

// extractText 拼接响应中的文本块，忽略 thinking / tool_use 等非文本块。
// 没有任何文本块时返回空串，由调用方决定如何兜底。
func extractText(msg *anthropic.Message) string {
	var parts []string
	for _, block := range msg.Content {
		if block.Type != textBlockType {
			continue
		}
		parts = append(parts, block.Text)
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}
