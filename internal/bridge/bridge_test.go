package bridge

import (
	"context"
	"sync"
	"testing"
	"time"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"justn0w-bot-bridge/internal/llm"
)

// stubLLM 是 answerer 的测试替身
type stubLLM struct {
	answer  *llm.Answer
	err     error
	panicOn bool
	gotQ    string
}

func (s *stubLLM) Ask(_ context.Context, question string) (*llm.Answer, error) {
	s.gotQ = question
	if s.panicOn {
		panic("模型调用 panic")
	}
	return s.answer, s.err
}

// sentMessage 记录一次发出的消息
type sentMessage struct {
	chatID string
	text   string
}

// stubFeishu 是 replier 的测试替身，通过 done 通知测试「已发出消息」
type stubFeishu struct {
	mu   sync.Mutex
	sent []sentMessage
	done chan struct{}
}

func newStubFeishu() *stubFeishu {
	return &stubFeishu{done: make(chan struct{}, 1)}
}

func (s *stubFeishu) SendText(_ context.Context, chatID, text string) error {
	s.mu.Lock()
	s.sent = append(s.sent, sentMessage{chatID: chatID, text: text})
	s.mu.Unlock()

	select {
	case s.done <- struct{}{}:
	default:
	}
	return nil
}

func (s *stubFeishu) sentMessages() []sentMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]sentMessage(nil), s.sent...)
}

// waitSent 等待至少一条消息发出，避免异步路径上测试抢跑
func (s *stubFeishu) waitSent(t *testing.T) {
	t.Helper()
	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
		t.Fatal("等待回复超时：answer goroutine 未发出消息")
	}
}

// blockingLLM 模拟慢模型，在 release 关闭前一直阻塞
type blockingLLM struct {
	entered chan struct{}
	release chan struct{}
}

func (s *blockingLLM) Ask(ctx context.Context, _ string) (*llm.Answer, error) {
	close(s.entered)
	select {
	case <-s.release:
		return &llm.Answer{Text: "迟到的答案"}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// validEvent 构造一条可被正常解析的群消息事件
func validEvent() *larkim.P2MessageReceiveV1 {
	return newEvent(strPtr("user"), strPtr("text"), strPtr(`{"text":"@_user_1 怎么申请调休"}`), strPtr("oc_chat"))
}

func TestAnswerRepliesWithModelText(t *testing.T) {
	model := &stubLLM{answer: &llm.Answer{Text: "先提交申请，再由主管审批。"}}
	feishu := newStubFeishu()

	New(model, feishu).answer(context.Background(), "oc_chat", "怎么申请调休")

	sent := feishu.sentMessages()
	if len(sent) != 1 {
		t.Fatalf("发出消息 %d 条, want 1 条", len(sent))
	}
	if sent[0].text != "先提交申请，再由主管审批。" {
		t.Errorf("回复内容 = %q, want 模型答案原文", sent[0].text)
	}
	if sent[0].chatID != "oc_chat" {
		t.Errorf("chatID = %q, want oc_chat（应回到原会话）", sent[0].chatID)
	}
}

func TestAnswerRepliesFallbackOnModelError(t *testing.T) {
	model := &stubLLM{err: context.DeadlineExceeded}
	feishu := newStubFeishu()

	New(model, feishu).answer(context.Background(), "oc_chat", "怎么申请调休")

	sent := feishu.sentMessages()
	if len(sent) != 1 {
		t.Fatalf("发出消息 %d 条, want 1 条", len(sent))
	}
	if sent[0].text != replyOnFailure {
		t.Errorf("回复内容 = %q, want 兜底话术 %q", sent[0].text, replyOnFailure)
	}
}

// TestAnswerRepliesFallbackOnEmptyText 空答案不能原样发出，
// 否则用户只会看到一条空白气泡
func TestAnswerRepliesFallbackOnEmptyText(t *testing.T) {
	model := &stubLLM{answer: &llm.Answer{Text: ""}}
	feishu := newStubFeishu()

	New(model, feishu).answer(context.Background(), "oc_chat", "怎么申请调休")

	sent := feishu.sentMessages()
	if len(sent) != 1 {
		t.Fatalf("发出消息 %d 条, want 1 条", len(sent))
	}
	if sent[0].text != replyOnFailure {
		t.Errorf("回复内容 = %q, want 兜底话术 %q", sent[0].text, replyOnFailure)
	}
}

// TestAnswerRecoversFromPanic 验证 panic 不会终止进程。
// 该 goroutine 不在飞书 SDK 的 recover 覆盖范围内。
func TestAnswerRecoversFromPanic(t *testing.T) {
	model := &stubLLM{panicOn: true}
	feishu := newStubFeishu()

	// panic 若逃逸，测试进程会直接崩溃，等于断言失败
	New(model, feishu).answer(context.Background(), "oc_chat", "怎么申请调休")

	if got := len(feishu.sentMessages()); got != 0 {
		t.Errorf("发出消息 %d 条, want 0 条（panic 后状态未知，不发消息）", got)
	}
}

func TestHandlerFeishuMsgSkipsInvalidEvent(t *testing.T) {
	model := &stubLLM{answer: &llm.Answer{Text: "不应被回复"}}
	feishu := newStubFeishu()

	// 机器人自己的消息，应被忽略
	botEvent := newEvent(strPtr("bot"), strPtr("text"), strPtr(`{"text":"我是机器人"}`), strPtr("oc_chat"))
	if err := New(model, feishu).HandlerFeishuMsg(context.Background(), botEvent); err != nil {
		t.Fatalf("HandlerFeishuMsg() = %v, want nil（解析失败只记日志，不影响 ACK）", err)
	}

	if got := len(feishu.sentMessages()); got != 0 {
		t.Errorf("发出消息 %d 条, want 0 条", got)
	}
	if model.gotQ != "" {
		t.Errorf("模型被调用且收到 %q, want 未被调用", model.gotQ)
	}
}

func TestHandlerFeishuMsgAnswersValidEvent(t *testing.T) {
	model := &stubLLM{answer: &llm.Answer{Text: "先提交申请，再由主管审批。"}}
	feishu := newStubFeishu()

	if err := New(model, feishu).HandlerFeishuMsg(context.Background(), validEvent()); err != nil {
		t.Fatalf("HandlerFeishuMsg() = %v, want nil", err)
	}

	feishu.waitSent(t)

	if model.gotQ != "怎么申请调休" {
		t.Errorf("模型收到的问题 = %q, want 剥离 @占位符 后的正文", model.gotQ)
	}
	if sent := feishu.sentMessages(); len(sent) != 1 || sent[0].chatID != "oc_chat" {
		t.Errorf("发出消息 = %+v, want 1 条且 chatID 为 oc_chat", sent)
	}
}

// TestHandlerFeishuMsgReturnsBeforeModelCompletes 固定住最关键的一条契约：
// handler 必须在模型返回前退出。飞书 SDK 在 handler 返回之后才写 ACK
// （oapi-sdk-go/ws/client_message.go），一旦在此同步等待模型，
// ACK 会被推迟数十秒，飞书判定未确认并重推，导致重复扣费与重复回复。
func TestHandlerFeishuMsgReturnsBeforeModelCompletes(t *testing.T) {
	slow := &blockingLLM{entered: make(chan struct{}), release: make(chan struct{})}
	feishu := newStubFeishu()
	b := New(slow, feishu)

	done := make(chan error, 1)
	go func() { done <- b.HandlerFeishuMsg(context.Background(), validEvent()) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("HandlerFeishuMsg() = %v, want nil", err)
		}
	case <-time.After(time.Second):
		close(slow.release)
		t.Fatal("HandlerFeishuMsg 被模型阻塞：应在模型返回前退出")
	}

	<-slow.entered      // 确认模型确实被调用了
	close(slow.release) // 放行，避免 goroutine 泄漏
	feishu.waitSent(t)

	if got := feishu.sentMessages()[0].text; got != "迟到的答案" {
		t.Errorf("回复内容 = %q, want 迟到的答案", got)
	}
}
