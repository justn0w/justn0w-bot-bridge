package claude

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// 单测一律用假 CLI，不碰真实的 claude：真实 CLI 依赖本机登录态、
// 会真的调用模型，既慢又不可重复。startProcess 留出的 cliPath 参数
// 就是为这里服务的。

// fakeCLI 在临时目录写入一个假 claude 脚本并返回其路径。
// script 是脚本主体，shebang 由本函数补上。
func fakeCLI(t *testing.T, script string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatalf("写入假 CLI 失败: %v", err)
	}
	return path
}

func TestStartProcessWritesEachLine(t *testing.T) {
	cli := fakeCLI(t, "echo 第一行\necho 第二行")

	var out bytes.Buffer
	if err := startProcess(cli, &out); err != nil {
		t.Fatalf("startProcess() = %v, want nil", err)
	}

	if want := "第一行\n第二行\n"; out.String() != want {
		t.Errorf("输出 = %q, want %q", out.String(), want)
	}
}

// TestStartProcessPassesPrompt 固定住「提问作为位置参数传给 CLI」这一约定。
// 注意：当前还没有 -p/--print 之类的参数，真实 claude 收到这种调用会进入
// 交互模式而非无头模式，接真 CLI 前需要先补上。
func TestStartProcessPassesPrompt(t *testing.T) {
	cli := fakeCLI(t, `echo "$1"`)

	var out bytes.Buffer
	if err := startProcess(cli, &out); err != nil {
		t.Fatalf("startProcess() = %v, want nil", err)
	}

	if want := prompt + "\n"; out.String() != want {
		t.Errorf("传给 CLI 的提问 = %q, want %q", out.String(), want)
	}
}

func TestStartProcessHandlesEmptyOutput(t *testing.T) {
	cli := fakeCLI(t, "true")

	var out bytes.Buffer
	if err := startProcess(cli, &out); err != nil {
		t.Fatalf("startProcess() = %v, want nil（无输出不算错误）", err)
	}
	if out.Len() != 0 {
		t.Errorf("输出 = %q, want 空", out.String())
	}
}

// laggingDelay 是 laggingWriter 每行的等待时长：要大到让读管道的一方稳定
// 落后于子进程的写，又要小到不拖慢测试套件。
const laggingDelay = 100 * time.Microsecond

// laggingWriter 每写一行都停一下，模拟下游较慢——实际场景里每一行都要发一次
// 飞书消息，读管道的一方必然落后于子进程的写。
type laggingWriter struct {
	mu    sync.Mutex
	lines int
}

func (w *laggingWriter) Write(p []byte) (int, error) {
	time.Sleep(laggingDelay)
	w.mu.Lock()
	w.lines++
	w.mu.Unlock()
	return len(p), nil
}

func (w *laggingWriter) Lines() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lines
}

// TestStartProcessDrainsBeforeReturning 固定住「返回时子进程的输出已全部写出」
// 这一约定。
//
// 旧实现把读管道放在 goroutine 里，主线程 cmd.Wait() 一返回就收工，
// 此时读方还可能落后一截，尾部输出就丢了。注意用快 writer（bytes.Buffer）
// 测不出来：子进程退出时写端先关闭，阻塞在 read() 上的读方随即拿到 EOF，
// 几乎总能抢在 Wait 关闭读端之前——窗口被下游慢才拉得开。
func TestStartProcessDrainsBeforeReturning(t *testing.T) {
	const lines = 3000
	cli := fakeCLI(t, fmt.Sprintf(
		"i=0; while [ $i -lt %d ]; do echo line-$i; i=$((i+1)); done", lines))

	out := &laggingWriter{}
	if err := startProcess(cli, out); err != nil {
		t.Fatalf("startProcess() = %v, want nil", err)
	}

	// startProcess 已返回，此处不再有并发写入，计数是确定的
	if got := out.Lines(); got != lines {
		t.Errorf("返回时已写出 %d 行, want %d（尾部输出被丢弃）", got, lines)
	}
}

func TestStartProcessErrorsWhenCLIMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-a-real-claude")

	var out bytes.Buffer
	err := startProcess(missing, &out)
	if err == nil {
		t.Fatal("startProcess() = nil, want error（CLI 不存在时应报错）")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("错误信息 = %q, want 含路径 %q", err.Error(), missing)
	}
}

func TestStartProcessErrorsOnNonZeroExit(t *testing.T) {
	cli := fakeCLI(t, "echo 部分输出\nexit 3")

	var out bytes.Buffer
	if err := startProcess(cli, &out); err == nil {
		t.Fatal("startProcess() = nil, want error（非零退出码应报错）")
	}

	// 退出前已产生的输出仍应保留，便于排查
	if !strings.Contains(out.String(), "部分输出") {
		t.Errorf("输出 = %q, want 含退出前已写入的部分", out.String())
	}
}

// TestStartProcessErrorsOnOverlongLine 固定住「扫描中断必须上报」这一约定。
// 扫描器中止与正常读完都会让 Scan() 返回 false，只有查 scanner.Err() 才分得清；
// 不查的话调用方拿到的是一份静默截断的输出，还以为自己读完了。
func TestStartProcessErrorsOnOverlongLine(t *testing.T) {
	// bufio.Scanner 默认单行上限 64KB，这里造一行 200KB
	cli := fakeCLI(t, `awk 'BEGIN{for(i=0;i<200000;i++)printf "a"; print ""}'`)

	var out bytes.Buffer
	if err := startProcess(cli, &out); err == nil {
		t.Error("startProcess() = nil, want error（单行超限应报错而非静默截断）")
	}
}
