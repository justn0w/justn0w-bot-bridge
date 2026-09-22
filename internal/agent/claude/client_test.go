package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// endMark 是假 CLI 写在 argv 记录里的分次标记。
// 取一个不可能是参数值的字符串，好把「每次调用的参数」从一条流水里切出来。
const endMark = "==END=="

// fakeScriptTemplate 是一个假 claude，落盘后可执行。
//
// 它把收到的 argv 逐行追加到 %[1]s，随后按各文件的内容决定行为：
// 睡眠（%[2]s）、stderr（%[3]s）、stdout（%[4]s）、退出码（%[5]s）。
// 把「行为」放在文件里而不是写死在脚本里，测试就能在同一个假 CLI 上
// 依次扮演成功、失败、超时，无需为每个用例重建脚本。
//
// 为什么真起一个脚本进程、而不是把 exec 抽成接口在 Go 侧 mock：
// 这一层要验的恰恰是「参数有没有真的进到 argv」「超时有没有真的杀掉子进程」，
// 只有真起进程才覆盖得到；mock 掉 exec 等于把要验的东西一起 mock 掉了。
const fakeScriptTemplate = `#!/bin/sh
for a in "$@"; do
  printf '%%s\n' "$a" >> "%[1]s"
done
echo "%[6]s" >> "%[1]s"
if [ -s "%[2]s" ]; then exec sleep "$(cat "%[2]s")"; fi
if [ -s "%[3]s" ]; then cat "%[3]s" >&2; fi
if [ -s "%[4]s" ]; then cat "%[4]s"; fi
exit "$(cat "%[5]s")"
`

// fakeCLI 承载假 CLI 的路径与各行为开关
type fakeCLI struct {
	path     string // 脚本路径，直接传给 Options.CLIPath
	argsFile string // 每次调用的 argv 逐行落盘，用 endMark 分隔
	fixture  string // stdout 内容
	stderr   string // stderr 内容
	delay    string // 睡眠秒数，用于制造超时
	exit     string // 退出码
}

func newFakeCLI(t *testing.T) *fakeCLI {
	t.Helper()

	dir := t.TempDir()
	f := &fakeCLI{
		path:     filepath.Join(dir, "claude"),
		argsFile: filepath.Join(dir, "args.txt"),
		fixture:  filepath.Join(dir, "fixture.json"),
		stderr:   filepath.Join(dir, "stderr.txt"),
		delay:    filepath.Join(dir, "delay.txt"),
		exit:     filepath.Join(dir, "exit.txt"),
	}

	script := fmt.Sprintf(fakeScriptTemplate,
		f.argsFile, f.delay, f.stderr, f.fixture, f.exit, endMark)
	if err := os.WriteFile(f.path, []byte(script), 0o755); err != nil {
		t.Fatalf("写入假 CLI 脚本失败: %v", err)
	}

	// 全部先置空。脚本里的判据是 -s（文件非空），空文件等于「该行为关闭」；
	// 退出码是唯一的例外，它总会被读取，空文件会让 sh 报 Illegal number。
	f.setFixture(t, "")
	f.setStderr(t, "")
	f.setDelay(t, "")
	f.setExit(t, 0)
	return f
}

func (f *fakeCLI) setFixture(t *testing.T, content string) {
	t.Helper()
	writeFile(t, f.fixture, content)
}

func (f *fakeCLI) setStderr(t *testing.T, content string) {
	t.Helper()
	writeFile(t, f.stderr, content)
}

func (f *fakeCLI) setDelay(t *testing.T, seconds string) {
	t.Helper()
	writeFile(t, f.delay, seconds)
}

func (f *fakeCLI) setExit(t *testing.T, code int) {
	t.Helper()
	writeFile(t, f.exit, fmt.Sprint(code))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写入 %s 失败: %v", path, err)
	}
}

// invocations 按调用顺序返回每次调用收到的 argv。
// 并发的用例里顺序不保证，只断言条数时才用它。
func (f *fakeCLI) invocations(t *testing.T) [][]string {
	t.Helper()

	raw, err := os.ReadFile(f.argsFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // 一次都没被调用过
		}
		t.Fatalf("读取 argv 记录失败: %v", err)
	}

	var out [][]string
	var cur []string
	for line := range strings.SplitSeq(string(raw), "\n") {
		switch line {
		case "", "\r":
			// strings.Split 在末尾换行处会切出空串，跳过
		case endMark:
			out = append(out, cur)
			cur = nil
		default:
			cur = append(cur, line)
		}
	}
	return out
}

// cliJSON 构造一份 CLI 的 json 输出信封。
// 复用生产的 cliResult 类型而不是手写字符串，fixture 就无从与解析端脱节。
func cliJSON(t *testing.T, result, sessionID string, isError bool) string {
	t.Helper()

	b, err := json.Marshal(cliResult{Result: result, SessionID: sessionID, IsError: isError})
	if err != nil {
		t.Fatalf("构造 fixture 失败: %v", err)
	}
	return string(b)
}

// resumeID 返回 argv 中 --resume 后面跟的值，没有则返回空串
func resumeID(args []string) string {
	for i, a := range args {
		if a == "--resume" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// newTestClient 用假 CLI 建一个 Client。
// 超时给得远宽于用例实际需要，免得测试成败取决于机器当时的负载。
func newTestClient(f *fakeCLI) *Client {
	return NewClient(Options{CLIPath: f.path, Timeout: 30 * time.Second})
}

// TestAskFirstCallSendsQuestionWithoutResume 覆盖首轮调用的 argv 组装与答案解析。
func TestAskFirstCallSendsQuestionWithoutResume(t *testing.T) {
	f := newFakeCLI(t)
	f.setFixture(t, cliJSON(t, "先提交申请，再由主管审批。", "sess-1", false))

	c := newTestClient(f)
	ans, err := c.Ask(context.Background(), "oc_chat", "怎么申请调休")
	if err != nil {
		t.Fatalf("Ask() = %v, want nil", err)
	}
	if ans.Text != "先提交申请，再由主管审批。" {
		t.Errorf("答案 = %q, want fixture 里的 result 原文", ans.Text)
	}

	inv := f.invocations(t)
	if len(inv) != 1 {
		t.Fatalf("调用 %d 次, want 1 次", len(inv))
	}

	// 首轮没有可续接的会话。带上 --resume 会让 CLI 直接报错退出，
	// 表现为「每个群的第一次提问都失败」。
	if got := resumeID(inv[0]); got != "" {
		t.Errorf("首轮 argv 带了 --resume %q, want 不带", got)
	}

	// -p 与 --output-format 缺一不可：前者决定单次问答模式，不加会掉进交互式 TUI
	// 然后一直等输入；后者决定 stdout 是 JSON 信封而非纯文本，不加则解析必失败。
	if !slices.Contains(inv[0], "-p") {
		t.Errorf("argv = %v, want 含 -p", inv[0])
	}
	if !slices.Contains(inv[0], "--output-format") || !slices.Contains(inv[0], "json") {
		t.Errorf("argv = %v, want 含 --output-format json", inv[0])
	}

	// 提问必须以 argv 元素的形式原样出现——这是「用户问的话真的送到了 CLI」
	// 的唯一证据。走 shell 拼接的实现会在这里露出马脚。
	if got := inv[0][len(inv[0])-1]; got != "怎么申请调休" {
		t.Errorf("argv 末位 = %q, want 提问原文", got)
	}
}

// TestAskSecondCallResumesPreviousSession 是「连续追问能接上上文」的全部实现证据：
// 第二轮必须把第一轮返回的 session_id 通过 --resume 传回去。
func TestAskSecondCallResumesPreviousSession(t *testing.T) {
	f := newFakeCLI(t)
	f.setFixture(t, cliJSON(t, "第一轮答案", "sess-A", false))

	c := newTestClient(f)
	if _, err := c.Ask(context.Background(), "oc_chat", "第一问"); err != nil {
		t.Fatalf("第一问失败: %v", err)
	}

	f.setFixture(t, cliJSON(t, "第二轮答案", "sess-A", false))
	if _, err := c.Ask(context.Background(), "oc_chat", "那第二点呢"); err != nil {
		t.Fatalf("第二问失败: %v", err)
	}

	inv := f.invocations(t)
	if len(inv) != 2 {
		t.Fatalf("调用 %d 次, want 2 次", len(inv))
	}
	if got := resumeID(inv[0]); got != "" {
		t.Errorf("首轮 --resume = %q, want 空", got)
	}
	if got := resumeID(inv[1]); got != "sess-A" {
		t.Errorf("次轮 --resume = %q, want sess-A（第一轮返回的 session_id）", got)
	}
}

// TestAskKeepsSessionsSeparatePerKey 验证会话按 key 分桶。
// 若 session 存成了全局唯一而非按 chatID 分桶，两个群的上下文会串在一起，
// 表现为「A 群的追问带动了 B 群的对话历史」。
func TestAskKeepsSessionsSeparatePerKey(t *testing.T) {
	f := newFakeCLI(t)
	c := newTestClient(f)

	f.setFixture(t, cliJSON(t, "A 群首答", "sess-a1", false))
	if _, err := c.Ask(context.Background(), "oc_a", "A 群第一问"); err != nil {
		t.Fatalf("A 群第一问失败: %v", err)
	}

	// B 群从未问过，首轮必须干净
	f.setFixture(t, cliJSON(t, "B 群首答", "sess-b1", false))
	if _, err := c.Ask(context.Background(), "oc_b", "B 群第一问"); err != nil {
		t.Fatalf("B 群第一问失败: %v", err)
	}

	f.setFixture(t, cliJSON(t, "B 群追问", "sess-b1", false))
	if _, err := c.Ask(context.Background(), "oc_b", "B 群第二问"); err != nil {
		t.Fatalf("B 群第二问失败: %v", err)
	}

	inv := f.invocations(t)
	if len(inv) != 3 {
		t.Fatalf("调用 %d 次, want 3 次", len(inv))
	}
	if got := resumeID(inv[0]); got != "" {
		t.Errorf("A 群首轮 --resume = %q, want 空", got)
	}
	if got := resumeID(inv[1]); got != "" {
		t.Errorf("B 群首轮 --resume = %q, want 空（不能被 A 群的会话污染）", got)
	}
	if got := resumeID(inv[2]); got != "sess-b1" {
		t.Errorf("B 群次轮 --resume = %q, want sess-b1", got)
	}
}

// TestAskClearsSessionWhenCallFails 验证失败后丢弃该群的会话。
//
// 这是自愈能力的来源：session_id 一旦失效（会话被清理、CLI 换代），
// 留着它这个群就会拿同一个死 id 反复 resume，永远失败、永远不自愈。
func TestAskClearsSessionWhenCallFails(t *testing.T) {
	f := newFakeCLI(t)
	f.setFixture(t, cliJSON(t, "第一轮答案", "sess-stale", false))

	c := newTestClient(f)
	if _, err := c.Ask(context.Background(), "oc_chat", "第一问"); err != nil {
		t.Fatalf("第一问失败: %v", err)
	}

	// 复现最典型的一类失败：resume 的 id 已失效。
	// CLI 的实际形态是退出码 1、stdout 为空、stderr 给出说明。
	f.setFixture(t, "")
	f.setStderr(t, "Failed to resume session: No conversation found with session ID sess-stale")
	f.setExit(t, 1)

	_, err := c.Ask(context.Background(), "oc_chat", "第二问")
	if err == nil {
		t.Fatal("CLI 非 0 退出时 Ask() = nil, want error")
	}
	// 失败原因必须从 stderr 透出来，否则线上只剩「exit status 1」，无从定位
	if !strings.Contains(err.Error(), "No conversation found") {
		t.Errorf("error = %v, want 含 CLI 的 stderr 说明", err)
	}

	f.setFixture(t, cliJSON(t, "第三轮答案", "sess-new", false))
	f.setStderr(t, "")
	f.setExit(t, 0)

	if _, err := c.Ask(context.Background(), "oc_chat", "第三问"); err != nil {
		t.Fatalf("第三问失败: %v", err)
	}

	inv := f.invocations(t)
	if len(inv) != 3 {
		t.Fatalf("调用 %d 次, want 3 次", len(inv))
	}
	if got := resumeID(inv[2]); got != "" {
		t.Errorf("失败后一轮 --resume = %q, want 空（失效的 session 应已被清掉）", got)
	}
}

// TestAskRejectsNonJSONOutput 覆盖 stdout 不是 JSON 信封的情形：
// CLI 升级换代改了输出格式、或异常时退回交互式界面刷屏。
func TestAskRejectsNonJSONOutput(t *testing.T) {
	f := newFakeCLI(t)
	f.setFixture(t, "这不是 JSON，只是普通文本")

	c := newTestClient(f)
	_, err := c.Ask(context.Background(), "oc_chat", "怎么申请调休")
	if err == nil {
		t.Fatal("stdout 不是合法 JSON 时 Ask() = nil, want error")
	}
	// 原始输出要带进错误里，否则「解析失败」这四个字对定位毫无帮助
	if !strings.Contains(err.Error(), "这不是 JSON") {
		t.Errorf("error = %v, want 含原始输出片段", err)
	}
}

// TestAskTreatsIsErrorAsFailure 验证 is_error=true 不会被当成答案发出去。
// 这类响应进程以 0 退出，只看退出码会把它当成功，
// 用户收到的就是一条错误信息而不是答疑内容。
func TestAskTreatsIsErrorAsFailure(t *testing.T) {
	f := newFakeCLI(t)
	f.setFixture(t, cliJSON(t, "Reached maximum number of turns (10)", "sess-1", true))

	c := newTestClient(f)
	_, err := c.Ask(context.Background(), "oc_chat", "怎么申请调休")
	if err == nil {
		t.Fatal("is_error=true 时 Ask() = nil, want error")
	}
	if !strings.Contains(err.Error(), "maximum number of turns") {
		t.Errorf("error = %v, want 含 result 里的错误说明", err)
	}
}

// TestAskKeepsDashedQuestionFromBeingParsedAsFlag 验证以 - 开头的提问不被吞掉。
// CLI 的参数解析器会把以 - 开头的位置参数当成选项，
// 用户问一句「-1 是什么意思」就会整轮失败。
func TestAskKeepsDashedQuestionFromBeingParsedAsFlag(t *testing.T) {
	f := newFakeCLI(t)
	f.setFixture(t, cliJSON(t, "答案", "sess-1", false))

	c := newTestClient(f)
	if _, err := c.Ask(context.Background(), "oc_chat", "-1 是什么意思"); err != nil {
		t.Fatalf("Ask() = %v, want nil", err)
	}

	inv := f.invocations(t)
	if len(inv) != 1 {
		t.Fatalf("调用 %d 次, want 1 次", len(inv))
	}
	last := inv[0][len(inv[0])-1]
	if strings.HasPrefix(last, "-") {
		t.Errorf("argv 末位 = %q，以 - 开头会被 CLI 当成未知选项", last)
	}
	// 规避手段不能把提问正文改坏：用户问的还是要原样送到
	if !strings.Contains(last, "-1 是什么意思") {
		t.Errorf("argv 末位 = %q, want 仍含提问正文", last)
	}
}

// TestAskReturnsPromptlyOnTimeout 验证超时真的终止了子进程，而不是干等它跑完。
func TestAskReturnsPromptlyOnTimeout(t *testing.T) {
	f := newFakeCLI(t)
	f.setFixture(t, cliJSON(t, "迟到的答案", "sess-1", false))
	f.setDelay(t, "10") // 远超下面的 200ms 超时

	c := NewClient(Options{CLIPath: f.path, Timeout: 200 * time.Millisecond})

	start := time.Now()
	_, err := c.Ask(context.Background(), "oc_chat", "怎么申请调休")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("超时后 Ask() = nil, want error")
	}
	// 关键断言：必须在超时点附近返回。若这里耗到了 10s，
	// 说明子进程没被 CommandContext 杀掉，线上就会堆起一批烧额度的孤儿进程。
	if elapsed > 5*time.Second {
		t.Errorf("耗时 %v，远超 200ms 超时：子进程可能没被终止", elapsed)
	}
	// 必须能判出「超时」而非「CLI 自己失败」——两者的处置方式不同
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want 可被 errors.Is 判为 context.DeadlineExceeded", err)
	}
}

// TestAskConcurrentKeysAreRaceFree 让多条飞书消息同时进来时的竞争浮出水面。
// 竞争由 -race 抓；这里断言的是并发下不丢调用。
func TestAskConcurrentKeysAreRaceFree(t *testing.T) {
	f := newFakeCLI(t)
	f.setFixture(t, cliJSON(t, "答案", "sess-1", false))

	c := newTestClient(f)

	// 每个 goroutine 用不同的群：避开「同一个 session 被并发 resume」这条
	// CLI 侧的限制，把测试聚焦在 sessions map 本身的并发安全上。
	const groups = 8
	var wg sync.WaitGroup
	for i := range groups {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("oc_%d", i)
			if _, err := c.Ask(context.Background(), key, "并发提问"); err != nil {
				t.Errorf("Ask(%s) = %v, want nil", key, err)
			}
		}(i)
	}
	wg.Wait()

	if got := len(f.invocations(t)); got != groups {
		t.Errorf("调用 %d 次, want %d 次（并发下不应丢调用）", got, groups)
	}
}
