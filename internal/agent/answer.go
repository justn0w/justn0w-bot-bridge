// Package agent 定义答疑 agent 层的中立契约。
//
// 单独成包是为了断开「消费方」与「实现方」之间的反向依赖：
// internal/bridge 与 internal/agent/claude 都只依赖这里，彼此不互相引用。
package agent

// Answer 是一次答疑的结构化结果
type Answer struct {
	Text      string // 答案正文
	ElapsedMS int64  // 调用耗时（毫秒），用于观测与超时分析
}
