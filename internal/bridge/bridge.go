// Package bridge 把飞书消息事件与答疑模型串起来：
// 解析提问 -> 调用模型 -> 把答案发回原会话。
//
// 不放在 cmd/server 下：cmd 层只保留进程装配（读配置、构造依赖、拉起长连接），
// 业务编排放在 internal 才能被独立测试，也不会把启动目录撑成一个杂物间。
package bridge

import (
	"context"
	"log"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"justn0w-bot-bridge/internal/agent"
)

const (
	// replyOnFailure 是调用模型失败时对用户的兜底话术，不外泄内部错误细节
	replyOnFailure = "抱歉，AI 答疑服务暂时不可用，请稍后重试或联系值班同学。"
)

// answerer 抽象答疑模型，便于测试替换。
//
// sessionKey 标识一段连续对话（当前传飞书 chatID）。答疑模型据此把同一群里的
// 多轮提问接到同一个上下文里；接口上没有这个键，实现端就无从分辨是谁在问。
type answerer interface {
	Ask(ctx context.Context, sessionKey, question string) (*agent.Answer, error)
}

// replier 抽象消息发送，便于测试替换
type replier interface {
	SendText(ctx context.Context, chatID, text string) error
}

// Bridge 聚合一次答疑所需的依赖。
// answerer 字段名与接口类型同名（Go 里常见的 logger Logger 写法），
// 既贴合它承载的东西，也避开与本文件 import 的包名 agent 撞名。
type Bridge struct {
	answerer answerer
	feishu   replier
}

// New 构造函数注入依赖。
// 参数用接口声明（accept interfaces），返回具体类型（return structs）。
func New(a answerer, r replier) *Bridge {
	return &Bridge{answerer: a, feishu: r}
}

// HandlerFeishuMsg 处理飞书 im.message.receive_v1 事件：
// 取出提问文本，调用模型生成答案，再把答案发回原会话。
//
// 注意：该方法必须尽快返回。飞书 SDK 在 handler 返回之后才写回 ACK
// （见 oapi-sdk-go/ws/client_message.go 中 handler.Do 之后的 writeEventResponse），
// 若在此同步等待模型，ACK 会被推迟数十秒，飞书将判定事件未确认并重推，
// 造成重复调用模型与重复回复。因此这里只做解析，实际处理交给 goroutine。
func (b *Bridge) HandlerFeishuMsg(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
	question, chatID, err := parseQuestion(event)
	if err != nil {
		// 解析失败只影响单条消息，记录即可，不应影响事件 ACK
		log.Printf("解析飞书消息失败，已跳过: %v", err)
		return nil
	}

	// 与事件生命周期解绑：handler 返回后 ctx 可能被取消
	go b.answer(context.WithoutCancel(ctx), chatID, question)
	return nil
}

// answer 调用模型并把结果发回会话
func (b *Bridge) answer(ctx context.Context, chatID, question string) {
	// 这里是独立 goroutine，不在飞书 SDK 的 panic 保护范围内
	// （handleMessageTask 的 recover 只覆盖 handler 本身），
	// 一旦 panic 会终止整个进程，因此需要自行兜底。
	defer func() {
		if r := recover(); r != nil {
			log.Printf("处理答疑时发生 panic: chat_id=%s: %v", chatID, r)
		}
	}()

	// chatID 同时也是会话键：同一个群的连续追问共享一段 CLI 会话
	ans, err := b.answerer.Ask(ctx, chatID, question)
	if err != nil {
		log.Printf("调用模型失败: chat_id=%s: %v", chatID, err)
		b.reply(ctx, chatID, replyOnFailure)
		return
	}
	if ans.Text == "" {
		// 模型可能只返回了非文本块；空消息发出去用户只会看到一条空白气泡
		log.Printf("模型返回空答案: chat_id=%s, cost_ms=%d", chatID, ans.ElapsedMS)
		b.reply(ctx, chatID, replyOnFailure)
		return
	}

	log.Printf("答疑完成: chat_id=%s, cost_ms=%d", chatID, ans.ElapsedMS)
	b.reply(ctx, chatID, ans.Text)
}

// reply 发送文本。失败时只能记日志——此刻已无其它渠道向用户兜底
func (b *Bridge) reply(ctx context.Context, chatID, text string) {
	if err := b.feishu.SendText(ctx, chatID, text); err != nil {
		log.Printf("回复飞书失败: chat_id=%s: %v", chatID, err)
	}
}
