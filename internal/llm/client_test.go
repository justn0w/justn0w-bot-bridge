package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// testModel 取 DeepSeek 的 v4 flash 档，与 config 默认值保持一致
const testModel = "deepseek-v4-flash"

// capturedRequest 记录桩服务收到的请求。
// handler 在独立 goroutine 中执行，因此用互斥锁保护，避免 -race 报告数据竞争。
type capturedRequest struct {
	mu   sync.Mutex
	path string
	body map[string]any
}

func (c *capturedRequest) record(path string, body map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.path = path
	c.body = body
}

func (c *capturedRequest) snapshot() (path string, body map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.path, c.body
}

// stubMessagesServer 模拟 Anthropic Messages 协议的服务端。
// capture 非空时回传收到的请求路径与请求体，便于断言实际发出的内容。
func stubMessagesServer(t *testing.T, status int, body string, capture *capturedRequest) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("解析请求体失败: %v", err)
			}
			capture.record(r.URL.Path, req)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if _, err := fmt.Fprint(w, body); err != nil {
			t.Errorf("写入响应失败: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// messagesResponse 构造一个最小可用的 Messages 响应
func messagesResponse(blocks ...map[string]any) string {
	raw, err := json.Marshal(map[string]any{
		"id":          "msg_test",
		"type":        "message",
		"role":        "assistant",
		"model":       testModel,
		"content":     blocks,
		"stop_reason": "end_turn",
		"usage":       map[string]int{"input_tokens": 10, "output_tokens": 20},
	})
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func newTestClient(baseURL string, timeout time.Duration) *Client {
	return NewClient(Options{
		APIKey:    "sk-test",
		BaseURL:   baseURL,
		Model:     testModel,
		MaxTokens: 2000,
		Timeout:   timeout,
	})
}

func TestAskExtractsTextAndSkipsNonTextBlocks(t *testing.T) {
	body := messagesResponse(
		map[string]any{"type": "thinking", "thinking": "内部推理不应外泄", "signature": "sig"},
		map[string]any{"type": "text", "text": "第一段"},
		map[string]any{"type": "text", "text": "第二段"},
	)
	srv := stubMessagesServer(t, http.StatusOK, body, nil)

	answer, err := newTestClient(srv.URL, 5*time.Second).Ask(context.Background(), "怎么申请调休？")
	if err != nil {
		t.Fatalf("Ask() = %v, want nil", err)
	}

	want := "第一段\n第二段"
	if answer.Text != want {
		t.Errorf("Answer.Text = %q, want %q", answer.Text, want)
	}
}

// TestAskPostsToMessagesPath 固定住「BaseURL 只填到域名」这一前提。
// SDK 会自行拼接 /v1/messages；若把 /v1 也写进 base_url，路径会变成
// /v1/v1/messages，DeepSeek 与 Anthropic 都会返回 404。
func TestAskPostsToMessagesPath(t *testing.T) {
	capture := &capturedRequest{}
	srv := stubMessagesServer(t, http.StatusOK, messagesResponse(map[string]any{"type": "text", "text": "ok"}), capture)

	if _, err := newTestClient(srv.URL, 5*time.Second).Ask(context.Background(), "你好"); err != nil {
		t.Fatalf("Ask() = %v, want nil", err)
	}

	path, _ := capture.snapshot()
	if path != "/v1/messages" {
		t.Errorf("请求路径 = %q, want %q", path, "/v1/messages")
	}
}

func TestAskSendsConfiguredParams(t *testing.T) {
	capture := &capturedRequest{}
	srv := stubMessagesServer(t, http.StatusOK, messagesResponse(map[string]any{"type": "text", "text": "ok"}), capture)

	if _, err := newTestClient(srv.URL, 5*time.Second).Ask(context.Background(), "你好"); err != nil {
		t.Fatalf("Ask() = %v, want nil", err)
	}

	_, req := capture.snapshot()

	if got := req["model"]; got != testModel {
		t.Errorf("model = %v, want %s", got, testModel)
	}
	if got := req["max_tokens"]; got != float64(2000) {
		t.Errorf("max_tokens = %v, want 2000", got)
	}

	system, ok := req["system"].([]any)
	if !ok || len(system) == 0 {
		t.Fatalf("system = %v, want 非空数组", req["system"])
	}
	if first, ok := system[0].(map[string]any); !ok || first["text"] == "" {
		t.Errorf("system[0] = %v, want 含 text 的提示词", system[0])
	}

	messages, ok := req["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("messages = %v, want 长度为 1 的数组", req["messages"])
	}
	msg, ok := messages[0].(map[string]any)
	if !ok {
		t.Fatalf("messages[0] = %v, want 对象", messages[0])
	}
	if msg["role"] != "user" {
		t.Errorf("messages[0].role = %v, want user", msg["role"])
	}
	content := msg["content"].([]any)[0].(map[string]any)
	if content["text"] != "你好" {
		t.Errorf("提问内容 = %v, want 你好", content["text"])
	}
}

func TestAskReturnsErrorOnServerError(t *testing.T) {
	srv := stubMessagesServer(t, http.StatusInternalServerError,
		`{"type":"error","error":{"type":"api_error","message":"boom"}}`, nil)

	if _, err := newTestClient(srv.URL, 5*time.Second).Ask(context.Background(), "你好"); err == nil {
		t.Error("Ask() = nil, want error（服务端返回 5xx 时应报错）")
	}
}

// TestAskErrorMentionsModel 确认错误信息里带上了模型名，
// 便于定位「接错 BaseURL」或「模型标识写错」这类配置问题。
func TestAskErrorMentionsModel(t *testing.T) {
	srv := stubMessagesServer(t, http.StatusBadRequest,
		`{"type":"error","error":{"type":"invalid_request_error","message":"model not found"}}`, nil)

	_, err := newTestClient(srv.URL, 5*time.Second).Ask(context.Background(), "你好")
	if err == nil {
		t.Fatal("Ask() = nil, want error")
	}
	if !strings.Contains(err.Error(), testModel) {
		t.Errorf("错误信息 = %q, want 含模型名 %s", err.Error(), testModel)
	}
}

func TestAskTimesOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 客户端超时后会取消请求，这里提前返回，避免拖慢 srv.Close()
		select {
		case <-time.After(3 * time.Second):
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)

	start := time.Now()
	_, err := newTestClient(srv.URL, 100*time.Millisecond).Ask(context.Background(), "你好")
	elapsed := time.Since(start)

	if err == nil {
		t.Error("Ask() = nil, want error（超时应报错）")
	}
	if elapsed > 2*time.Second {
		t.Errorf("Ask() 耗时 %v，超时未生效（应约 100ms 就返回）", elapsed)
	}
}

func TestAskReturnsEmptyTextWhenNoTextBlocks(t *testing.T) {
	body := messagesResponse(map[string]any{"type": "thinking", "thinking": "只有推理块", "signature": "sig"})
	srv := stubMessagesServer(t, http.StatusOK, body, nil)

	answer, err := newTestClient(srv.URL, 5*time.Second).Ask(context.Background(), "你好")
	if err != nil {
		t.Fatalf("Ask() = %v, want nil", err)
	}
	if answer.Text != "" {
		t.Errorf("Answer.Text = %q, want 空串（无文本块时由调用方兜底）", answer.Text)
	}
}
