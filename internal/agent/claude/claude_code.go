package claude

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
)

// prompt 是当前写死的提问，后续由调用方传入
const prompt = "who you are ?"

// startProcess 拉起 cliPath 指定的进程，把它的 stdout 逐行写入 out。
//
// cliPath 与 out 是刻意留出的两个缝：前者让测试能注入假 CLI，不必依赖本机
// 安装并登录真实 claude；后者让断言落在调用方传入的 buffer 上，不必替换
// 全局的 os.Stdout。
func startProcess(cliPath string, out io.Writer) error {
	// -p/--print 是必须的：不带它 claude 会进交互式 TUI 并等待终端输入，
	// 而这里的 stdin 是 /dev/null、stdout 是管道，结果只会是立即 EOF 或卡死。
	cmd := exec.Command(cliPath, "-p", prompt)
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("获取 stdout 管道失败: %w", err)
	}

	scanner := bufio.NewScanner(stdoutPipe)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 %s 失败: %w", cliPath, err)
	}

	readErr := scanLines(scanner, out)
	if readErr != nil {
		// 读取提前失败时子进程可能仍阻塞在写管道上永不退出，
		// 先终止再回收，否则会留下僵尸进程
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()

	if readErr != nil {
		return fmt.Errorf("读取 %s 输出失败: %w", cliPath, readErr)
	}
	if waitErr != nil {
		return fmt.Errorf("%s 退出异常: %w", cliPath, waitErr)
	}
	return nil
}

// scanLines 把扫描器读到的每一行写入 out，返回扫描过程中的首个错误。
//
// 读循环必须与 cmd.Wait() 分开：Wait 会关闭管道读端，两者并发执行时
// 尾部输出会被 ErrClosed 吞掉。同时这里显式返回 scanner.Err()，
// 避免单行超限之类的扫描错误被静默丢弃。
func scanLines(scanner *bufio.Scanner, out io.Writer) error {
	for scanner.Scan() {
		if _, err := fmt.Fprintln(out, scanner.Text()); err != nil {
			return err
		}
	}
	return scanner.Err()
}
