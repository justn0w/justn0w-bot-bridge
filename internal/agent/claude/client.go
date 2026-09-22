// Package claude 通过 Claude Code CLI 提供答疑能力。
//
// 走 CLI 而非 HTTP 直连模型：好处是直接复用本机登录态、工具链与代码库上下文，
// 代价是每次提问都要拉起一个子进程。
//
// 上下文延续依赖 CLI 自己的 session：每问一次解析出 session_id 存下来，
// 下一问带 --resume 续接，因此同一个群里可以连续追问。
package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"justn0w-bot-bridge/internal/agent"
)

// defaultTimeout 是单次 CLI 调用的兜底超时。
// 与 config.yaml 中 claude.timeout_sec 的默认值保持一致。
const defaultTimeout = 120 * time.Second

// maxLogBytes 限制错误信息里回显的 CLI 原始输出长度，
// 避免一整屏 JSON 灌进日志
const maxLogBytes = 512

// Options 是构造 Client 所需的配置。
// 独立于 internal/config，避免包之间产生反向依赖。
type Options struct {
	// CLIPath 是 claude 可执行文件路径，留空则按 "claude" 走 PATH 查找
	CLIPath string
	// WorkDir 是子进程的工作目录，也是 CLI 读取代码库上下文的根。
	// 留空则继承本进程当前目录。
	//
	// 会话续接强依赖这个值：CLI 把会话记录存在
	// ~/.claude/projects/<按 cwd 转义出的目录名>/<session_id>.jsonl，
	// 而 --resume 是按 cwd 做前缀匹配去找的。**工作目录一变，之前存的
	// session_id 就接不上**，表现为退出码 1 且 stderr 报
	// 「No conversation found with session ID」。服务重启时务必沿用同一目录，
	// 否则每个群都要先白白失败一轮（Ask 失败会清掉会话，之后才自愈）。
	WorkDir string
	// Timeout 是单次调用超时，留空则用 defaultTimeout
	Timeout time.Duration
}

// Client 按会话维护 CLI 的 session_id
type Client struct {
	cliPath string
	workDir string
	timeout time.Duration

	// mu 保护 sessions。Ask 由每条飞书消息各自的 goroutine 调用，
	// 天然并发，map 不加锁会直接被 -race 抓出来。
	mu       sync.Mutex
	sessions map[string]string // sessionKey（飞书 chatID）-> CLI session_id
}

// NewClient 创建 CLI 答疑客户端
func NewClient(opts Options) *Client {
	if opts.CLIPath == "" {
		opts.CLIPath = "claude"
	}
	if opts.Timeout <= 0 {
		opts.Timeout = defaultTimeout
	}
	return &Client{
		cliPath:  opts.CLIPath,
		workDir:  opts.WorkDir,
		timeout:  opts.Timeout,
		sessions: make(map[string]string),
	}
}

// cliResult 是 claude -p --output-format json 输出信封中我们关心的字段。
// 字段名与 CLI 的实际输出强绑定；若 CLI 升级后改名，只需改这一处。
type cliResult struct {
	Result    string `json:"result"`     // 答案正文
	SessionID string `json:"session_id"` // 会话标识，供下一次 --resume 使用
	IsError   bool   `json:"is_error"`   // 进程以 0 退出、但答案本身是错误时的标记
}

// Ask 提交一次提问并返回答案。
//
// sessionKey 标识一段连续对话（当前传入飞书 chatID）：同一个 key 的后续提问
// 会带上上一次的 session_id 续接上下文，不同 key 之间互不串扰。
func (c *Client) Ask(ctx context.Context, sessionKey, question string) (*agent.Answer, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	start := time.Now()

	// 失败即丢弃该群的会话。把这条不变量收在一个 defer 里，而不是散落在
	// 每个 error 分支上：漏掉任何一处，那个群都会带着一个已经失效的 id
	// 反复失败——最常见的一类失败恰恰就是 --resume 的 id 已失效
	// （会话被清理、CLI 升级换代），这种 id 永远不会自己好起来。
	// 代价是超时也会丢上下文，MVP 阶段可接受。
	succeeded := false
	defer func() {
		if !succeeded {
			c.setSession(sessionKey, "")
		}
	}()

	args := []string{"-p", "--output-format", "json"}
	if sid := c.session(sessionKey); sid != "" {
		args = append(args, "--resume", sid)
	}
	args = append(args, promptArg(question))

	out, err := runCLI(ctx, c.cliPath, c.workDir, args)
	if err != nil {
		return nil, err
	}

	var res cliResult
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, fmt.Errorf("解析 %s 输出失败: %w（原始输出: %s）", c.cliPath, err, truncateForLog(out))
	}
	if res.IsError {
		// 进程正常退出但结果本身是错误，直接当答案发出去会把错误信息
		// 当成答疑内容回给用户
		return nil, fmt.Errorf("%s 返回错误结果: %s", c.cliPath, truncateForLog([]byte(res.Result)))
	}

	// 续接轮次返回的 session_id 可能与传入值不同，一律以本次返回的为准
	succeeded = true
	c.setSession(sessionKey, res.SessionID)

	return &agent.Answer{
		Text:      strings.TrimSpace(res.Result),
		ElapsedMS: time.Since(start).Milliseconds(),
	}, nil
}

// promptArg 把提问整理成一个不会被 CLI 当成选项解析的 argv 元素。
//
// 提问来自飞书群消息，是外部输入。CLI 的参数解析器会把以 "-" 开头的位置参数
// 视作选项，用户发一句「-1 是什么意思」就会被判成未知选项而整轮失败。
// 加一个前导空格即可让首字符不再是 "-"，而这段空白对模型没有语义。
func promptArg(question string) string {
	if strings.HasPrefix(question, "-") {
		return " " + question
	}
	return question
}

// session 读取某段会话的 CLI session_id。空 key 视为不启用会话。
func (c *Client) session(key string) string {
	if key == "" {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessions[key]
}

// setSession 写入 session_id，传空串表示清除该会话
func (c *Client) setSession(key, id string) {
	if key == "" {
		return
	}

	id = strings.TrimSpace(id)

	c.mu.Lock()
	defer c.mu.Unlock()
	if id == "" {
		delete(c.sessions, key)
		return
	}
	c.sessions[key] = id
}

// truncateForLog 截断用于错误信息的输出。
// 按 rune 边界收口，避免把一个多字节汉字劈成半个、在日志里显示成乱码。
func truncateForLog(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) <= maxLogBytes {
		return s
	}

	cut := s[:maxLogBytes]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut + "…（已截断）"
}
