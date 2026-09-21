package claude

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// 接真 claude 的冒烟测试，默认跳过。
//
// 之所以默认跳过：它依赖本机登录态、会真实调用模型并消耗额度、单次十几秒起步。
// 放进默认 go test ./... 会让 CI 既慢又不可重复。显式打开开关才跑：
//
//	REAL_CLAUDE=1 go test ./internal/agent/claude/ -run TestStartProcessRealCLI -v
//
// 断言刻意落在「管道通不通」上，而不是模型说了什么：模型输出每次都不一样，
// 把它写进断言等于给自己埋一个必炸的 flaky 测试。
func TestStartProcessRealCLI(t *testing.T) {
	if os.Getenv("REAL_CLAUDE") == "" {
		t.Skip("跳过真 CLI 测试；设 REAL_CLAUDE=1 启用")
	}

	cli, err := exec.LookPath("claude")
	if err != nil {
		t.Skipf("PATH 中找不到 claude，无法做真 CLI 验证: %v", err)
	}

	var out bytes.Buffer
	if err := startProcess(cli, &out); err != nil {
		t.Fatalf("startProcess(%q) = %v, want nil", cli, err)
	}

	got := out.String()

	// 空输出是最典型的失败形态：进程成功退出但 stdout 管道没接上，
	// 或者 -p 缺失导致 claude 根本没产出。必须显式拦下。
	if strings.TrimSpace(got) == "" {
		t.Fatal("输出为空，stdout 管道可能没接上，或 -p 没有生效")
	}

	// prompt 要的是 hello world 程序，正常回答必然出现该字样。
	// 大小写不敏感，避免被模型的大小写风格差异误伤。
	if !strings.Contains(strings.ToLower(got), "hello") {
		t.Errorf("输出中未出现 hello，实际输出:\n%s", got)
	}

	// 至少两行才能证明 scanLines 真的在逐行拆分，而不是把整块缓冲一次吐出。
	if lines := strings.Count(strings.TrimRight(got, "\n"), "\n") + 1; lines < 2 {
		t.Errorf("只解析出 %d 行，scanLines 可能未逐行拆分:\n%q", lines, got)
	}

	t.Logf("真实 CLI 输出 %d 字节:\n%s", out.Len(), got)
}
