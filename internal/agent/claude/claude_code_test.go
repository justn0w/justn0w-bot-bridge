package claude

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestBoundedBufferDropsBeyondLimit 固定住「子进程输出失控时不会吃光内存」这条底线。
//
// 写满之后仍要继续接受写入并返回成功，而不是回一个写错误：子进程若被 Write
// 的错误提前打断，反而会把真正的失败原因掩盖掉（见 boundedBuffer 的函数注释）。
func TestBoundedBufferDropsBeyondLimit(t *testing.T) {
	var b boundedBuffer
	b.max = 8

	// 未超限：原样收下，不置截断标记
	if n, err := b.Write([]byte("12345")); n != 5 || err != nil {
		t.Fatalf("Write() = (%d, %v), want (5, nil)", n, err)
	}
	if b.truncated {
		t.Error("未超限就置了 truncated")
	}

	// 溢出：只剩 3 字节空间，但必须报告「写满了 5 字节」——
	// 返回短写会让子进程收到 io.ErrShortWrite
	if n, err := b.Write([]byte("67890")); n != 5 || err != nil {
		t.Fatalf("Write() = (%d, %v), want (5, nil)：溢出也不应报写错误", n, err)
	}
	if !b.truncated {
		t.Error("超限后 truncated = false, want true")
	}
	if got := b.buf.String(); got != "12345678" {
		t.Errorf("缓冲内容 = %q, want 截断到 12345678", got)
	}

	// 已满之后再写，仍要接受
	if n, err := b.Write([]byte("x")); n != 1 || err != nil {
		t.Errorf("满载后 Write() = (%d, %v), want (1, nil)", n, err)
	}
	if got := b.buf.String(); got != "12345678" {
		t.Errorf("满载后缓冲内容 = %q, want 不变", got)
	}
}

// TestRunCLIReportsUnavailableBinary 覆盖 cli_path 配错的情形。
// 这条路径必须返回 error 而不是空输出——否则 Ask 会拿空 stdout 去解析 JSON，
// 报出「解析失败」，把排查方向指到完全无关的地方。
func TestRunCLIReportsUnavailableBinary(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-cli")

	out, err := runCLI(context.Background(), missing, "", []string{"-p", "hi"})
	if err == nil {
		t.Fatalf("runCLI() = (%q, nil), want error", out)
	}
	if len(out) != 0 {
		t.Errorf("失败时返回了输出 %q, want 空", out)
	}
	// 错误里要能看出是哪个可执行文件找不到，否则多环境部署时只能靠猜
	if !strings.Contains(err.Error(), "no-such-cli") {
		t.Errorf("error = %v, want 含可执行文件路径", err)
	}
}

// TestRunCLIReportsTimeoutAsContextError 固定住那条不起眼但关键的补刀：
// CommandContext 超时杀掉子进程后，Wait 返回的是「signal: killed」这类退出错误，
// 并不保证包装 context.DeadlineExceeded；不显式回看 ctx，调用方就区分不出
// 「超时」与「CLI 自己失败」，而这两者的处置方式不同。
func TestRunCLIReportsTimeoutAsContextError(t *testing.T) {
	f := newFakeCLI(t)
	f.setDelay(t, "10")

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err := runCLI(ctx, f.path, "", []string{"-p", "hi"})
	if err == nil {
		t.Fatal("runCLI() = nil, want error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want 可被 errors.Is 判为 context.DeadlineExceeded", err)
	}
}

// TestAskRealCLI 是接真 claude 的冒烟测试，默认跳过。
//
// 之所以默认跳过：它依赖本机登录态、会真实调用模型并消耗额度、单次十几秒起步。
// 放进默认 go test ./... 会让 CI 既慢又不可重复。显式打开开关才跑：
//
//	REAL_CLAUDE=1 go test ./internal/agent/claude/ -run TestAskRealCLI -v
//
// 断言刻意落在「管道通不通」上，而不是模型说了什么：模型输出每次都不一样，
// 把它写进断言等于给自己埋一个必炸的 flaky 测试。
func TestAskRealCLI(t *testing.T) {
	if os.Getenv("REAL_CLAUDE") == "" {
		t.Skip("跳过真 CLI 测试；设 REAL_CLAUDE=1 启用")
	}

	cli, err := exec.LookPath("claude")
	if err != nil {
		t.Skipf("PATH 中找不到 claude，无法做真 CLI 验证: %v", err)
	}

	c := NewClient(Options{CLIPath: cli, Timeout: 3 * time.Minute})

	ans, err := c.Ask(context.Background(), "smoke", "用一句话说明你当前具备哪些能力")
	if err != nil {
		t.Fatalf("Ask() = %v, want nil", err)
	}

	// 空答案是最典型的失败形态：进程成功退出但 stdout 管道没接上，
	// 或者 --output-format json 没生效导致解析不出正文。
	if strings.TrimSpace(ans.Text) == "" {
		t.Fatal("答案为空，stdout 管道可能没接上，或 --output-format json 没生效")
	}
	if ans.ElapsedMS <= 0 {
		t.Errorf("ElapsedMS = %d, want > 0", ans.ElapsedMS)
	}

	// session_id 必须落到 sessions 里，否则「每群一个会话」形同虚设：
	// 下一轮追问会因为读不到 id 而退回全新会话，丢失上下文。
	if got := c.session("smoke"); got == "" {
		t.Error("sessions 中未存下 session_id，后续追问将接不上上下文")
	}

	t.Logf("真实 CLI 答案（耗时 %dms）:\n%s", ans.ElapsedMS, ans.Text)
}
