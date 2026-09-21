// Package feishu 封装飞书开放接口的消息发送能力。
package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

const (
	// receiveIDTypeChatID 表示以群 ID 作为消息接收者。
	// 该 SDK 版本未导出对应常量，故在此定义。
	receiveIDTypeChatID = "chat_id"

	// requestTimeout 是单次 HTTP 请求的超时。
	// 不设置时 SDK 会回落到无超时的 http.DefaultClient，
	// 而调用方传入的 context 可能是无 deadline 的，连接卡住会导致 goroutine 永久泄漏。
	requestTimeout = 10 * time.Second
)

// Client 封装飞书消息发送
type Client struct {
	api *lark.Client
}

// NewClient 创建飞书客户端（凭证不入代码库，由 .env 注入）
func NewClient(appID, appSecret string) *Client {
	return &Client{api: lark.NewClient(appID, appSecret, lark.WithReqTimeout(requestTimeout))}
}

// textContent 是文本消息 content 字段的 JSON 结构
type textContent struct {
	Text string `json:"text"`
}

// SendText 向指定会话发送文本消息。
// SDK 只接受 content 的 JSON 字符串，这里负责序列化。
func (c *Client) SendText(ctx context.Context, chatID, text string) error {
	content, err := json.Marshal(textContent{Text: text})
	if err != nil {
		return fmt.Errorf("序列化消息内容失败: %w", err)
	}

	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(receiveIDTypeChatID).
		Body(&larkim.CreateMessageReqBody{
			ReceiveId: larkcore.StringPtr(chatID),
			MsgType:   larkcore.StringPtr(larkim.MsgTypeText),
			Content:   larkcore.StringPtr(string(content)),
		}).
		Build()

	resp, err := c.api.Im.Message.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("发送飞书消息失败: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("发送飞书消息失败: code=%d, msg=%s", resp.Code, resp.Msg)
	}
	return nil
}
