package bridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// senderTypeBot 标识消息由机器人自己发出，需要忽略以避免自问自答的死循环
const senderTypeBot = "bot"

// mentionPattern 匹配飞书文本中的 @某人 占位符，形如 @_user_1
var mentionPattern = regexp.MustCompile(`@_user_\d+`)

// parseQuestion 从消息事件中取出提问文本与会话 ID。
// 非文本消息、机器人自己的消息、空提问都返回错误，由调用方跳过。
func parseQuestion(event *larkim.P2MessageReceiveV1) (question, chatID string, err error) {
	if event == nil || event.Event == nil {
		return "", "", errors.New("事件内容为空")
	}
	if sender := event.Event.Sender; sender != nil && sender.SenderType != nil && *sender.SenderType == senderTypeBot {
		return "", "", errors.New("忽略机器人自己发出的消息")
	}

	msg := event.Event.Message
	if msg == nil {
		return "", "", errors.New("事件缺少消息体")
	}
	if msg.ChatId == nil || *msg.ChatId == "" {
		return "", "", errors.New("事件缺少 chat_id")
	}
	if msg.MessageType == nil || *msg.MessageType != larkim.MsgTypeText {
		return "", "", fmt.Errorf("暂不支持的消息类型: %s", derefString(msg.MessageType))
	}
	if msg.Content == nil {
		return "", "", errors.New("事件缺少消息内容")
	}

	var content struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(*msg.Content), &content); err != nil {
		return "", "", fmt.Errorf("解析消息内容失败: %w", err)
	}

	text := stripMentions(content.Text)
	if text == "" {
		return "", "", errors.New("提问内容为空")
	}
	return text, *msg.ChatId, nil
}

// stripMentions 去掉 @机器人 留下的 @_user_N 占位符，只保留真正的提问内容。
// 只删占位符本身，保留换行与缩进——用户常把堆栈或代码片段整段贴进来，
// 折叠空白会把多行内容压成一行，直接损害答疑质量。
func stripMentions(text string) string {
	return strings.TrimSpace(mentionPattern.ReplaceAllString(text, ""))
}

// derefString 安全解引用可空字符串，仅用于日志与错误信息
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
