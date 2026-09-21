package bridge

import (
	"testing"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func strPtr(s string) *string { return &s }

// newEvent 构造消息事件，传 nil 表示该字段在事件中缺失
func newEvent(senderType, messageType, content, chatID *string) *larkim.P2MessageReceiveV1 {
	return &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{SenderType: senderType},
			Message: &larkim.EventMessage{
				ChatId:      chatID,
				MessageType: messageType,
				Content:     content,
			},
		},
	}
}

func TestParseQuestion(t *testing.T) {
	tests := []struct {
		name     string
		event    *larkim.P2MessageReceiveV1
		wantQ    string
		wantChat string
		wantErr  bool
	}{
		{
			name:     "去掉 @机器人 占位符后取到提问",
			event:    newEvent(strPtr("user"), strPtr("text"), strPtr(`{"text":"@_user_1 怎么申请调休"}`), strPtr("oc_chat")),
			wantQ:    "怎么申请调休",
			wantChat: "oc_chat",
		},
		{
			name:     "多个 @ 占位符全部剥离且保留正文",
			event:    newEvent(strPtr("user"), strPtr("text"), strPtr(`{"text":"@_user_1 @_user_2 帮我看看这个报错"}`), strPtr("oc_chat")),
			wantQ:    "帮我看看这个报错",
			wantChat: "oc_chat",
		},
		{
			name:     "发送者类型缺失时仍按用户消息处理",
			event:    newEvent(nil, strPtr("text"), strPtr(`{"text":"你好"}`), strPtr("oc_chat")),
			wantQ:    "你好",
			wantChat: "oc_chat",
		},
		{
			name:    "忽略机器人自己的消息，避免死循环",
			event:   newEvent(strPtr("bot"), strPtr("text"), strPtr(`{"text":"我是机器人"}`), strPtr("oc_chat")),
			wantErr: true,
		},
		{
			name:    "非文本消息直接跳过",
			event:   newEvent(strPtr("user"), strPtr("image"), strPtr(`{"image_key":"img_x"}`), strPtr("oc_chat")),
			wantErr: true,
		},
		{
			name:    "只有 @机器人 没有正文时视为空提问",
			event:   newEvent(strPtr("user"), strPtr("text"), strPtr(`{"text":"@_user_1"}`), strPtr("oc_chat")),
			wantErr: true,
		},
		{
			name:    "缺少 chat_id 时报错",
			event:   newEvent(strPtr("user"), strPtr("text"), strPtr(`{"text":"你好"}`), nil),
			wantErr: true,
		},
		{
			name:    "缺少 message_type 时报错",
			event:   newEvent(strPtr("user"), nil, strPtr(`{"text":"你好"}`), strPtr("oc_chat")),
			wantErr: true,
		},
		{
			name:    "content 非法 JSON 时报错",
			event:   newEvent(strPtr("user"), strPtr("text"), strPtr(`not-json`), strPtr("oc_chat")),
			wantErr: true,
		},
		{
			name:    "缺少 content 时报错",
			event:   newEvent(strPtr("user"), strPtr("text"), nil, strPtr("oc_chat")),
			wantErr: true,
		},
		{
			name:    "事件内容为空时报错",
			event:   &larkim.P2MessageReceiveV1{},
			wantErr: true,
		},
		{
			name:    "事件为 nil 时报错",
			event:   nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			question, chatID, err := parseQuestion(tt.event)

			if (err != nil) != tt.wantErr {
				t.Fatalf("parseQuestion() error = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if question != tt.wantQ {
				t.Errorf("question = %q, want %q", question, tt.wantQ)
			}
			if chatID != tt.wantChat {
				t.Errorf("chatID = %q, want %q", chatID, tt.wantChat)
			}
		})
	}
}

func TestStripMentions(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "无占位符时原样返回", in: "怎么申请调休", want: "怎么申请调休"},
		{name: "剥离单个占位符", in: "@_user_1 怎么申请调休", want: "怎么申请调休"},
		{name: "剥离多个占位符", in: "@_user_1 @_user_2 帮忙看下", want: "帮忙看下"},
		{name: "只有占位符时返回空串", in: "@_user_1", want: ""},
		{name: "空串返回空串", in: "", want: ""},
		// 正文里出现的 @ 不应被误删，只删 @_user_N 占位符
		{name: "保留正文中的普通 @", in: "@_user_1 联系 @张三", want: "联系 @张三"},
		// 用户常整段粘贴堆栈/代码，换行与缩进必须原样传给模型
		{
			name: "保留换行与缩进",
			in:   "@_user_1 这个报错怎么解决\n\npanic: nil pointer\n\tmain.go:42",
			want: "这个报错怎么解决\n\npanic: nil pointer\n\tmain.go:42",
		},
		{
			name: "保留多行代码块",
			in:   "@_user_1 ```go\nfunc main() {}\n```",
			want: "```go\nfunc main() {}\n```",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripMentions(tt.in); got != tt.want {
				t.Errorf("stripMentions(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
