package claude

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// 子进程输出上限。正常回答是单个 JSON 对象，量级在几 KB；
// 一旦逼近这些上限，说明 CLI 行为异常（例如退回交互式界面刷屏），
// 继续读下去只会把服务进程的内存吃光。
const (
	maxStdoutBytes = 1 << 20 // 1 MiB
	maxStderrBytes = 64 << 10
)

// runCLI 执行一次 claude 调用，返回 stdout 原文。
//
// 三个刻意的设计：
//
//   - args 以切片给出，提问作为其中一个元素直接进 argv，全程不经过 shell。
//     提问来自飞书群消息，属外部输入；一旦改成拼字符串再交给 sh -c，就是命令注入。
//   - 用 exec.CommandContext 而非 exec.Command。调用方的超时一旦触发，
//     子进程必须被一起终止，否则会留下继续消耗额度的孤儿进程。
//   - 走 cmd.Stdout 赋值而不是 cmd.StdoutPipe()。exec 包会为前者起一个拷贝
//     goroutine 并让 Wait 等它读完；后者则要求自行保证「先读完再 Wait」——
//     Wait 会关闭管道读端，并发执行时尾部输出会被 ErrClosed 吞掉。
func runCLI(ctx context.Context, cliPath, workDir string, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, cliPath, args...)
	if workDir != "" {
		cmd.Dir = workDir
	}

	var stdout, stderr boundedBuffer
	stdout.max, stderr.max = maxStdoutBytes, maxStderrBytes
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	// 不接 stdin（保持 nil，即 /dev/null）：-p 模式下 stdin 是 prompt 的备选来源，
	// 留着管道会让 CLI 误以为要读流式输入。
	if err := cmd.Run(); err != nil {
		// 显式回看 ctx：CommandContext 超时杀掉子进程后，Wait 返回的是
		// 「signal: killed」这类退出错误，并不保证包装 context.DeadlineExceeded。
		// 不在这里补一刀，调用方就无从区分「超时」与「CLI 自己失败」。
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("执行 %s 被中断: %w", cliPath, ctxErr)
		}
		return nil, fmt.Errorf("执行 %s 失败: %w（stderr: %s）", cliPath, err, stderr.text())
	}

	if stdout.truncated {
		return nil, fmt.Errorf("执行 %s 的输出超过 %d 字节上限，已丢弃", cliPath, maxStdoutBytes)
	}
	return stdout.buf.Bytes(), nil
}

// boundedBuffer 收集子进程输出，超出上限后丢弃后续内容。
//
// 丢弃而非返回写错误：子进程若被 Write 的错误打断，可能直接退出，
// 反而掩盖掉真正的失败原因。这里让进程正常跑完，再由调用方根据 truncated 决定是否采信。
type boundedBuffer struct {
	buf       bytes.Buffer
	max       int
	truncated bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	remain := b.max - b.buf.Len()
	if remain <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if len(p) > remain {
		b.buf.Write(p[:remain])
		b.truncated = true
		return len(p), nil
	}
	b.buf.Write(p)
	return len(p), nil
}

// text 返回已收集内容，仅用于拼接错误信息
func (b *boundedBuffer) text() string {
	return strings.TrimSpace(b.buf.String())
}
