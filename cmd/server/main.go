package main

import (
	"context"
	"log"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"

	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"

	"justn0w-bot-bridge/internal/agent/claude"
	"justn0w-bot-bridge/internal/bridge"
	"justn0w-bot-bridge/internal/config"
	"justn0w-bot-bridge/internal/feishu"
)

func main() {
	// 1. 加载配置（.env + configs/config.yaml）
	// 必填项只有飞书凭证：答疑走本机 CLI，不需要模型密钥。
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 2. 组装答疑依赖：CLI 生成答案，飞书回发消息
	//
	// 走本机 Claude Code CLI：直接复用 CLI 的登录态、工具链与代码库上下文，
	// 代价是每次提问起一个子进程；运行机器需已登录 Claude Code。
	//
	// WorkDir 刻意留空，即继承启动目录——CLI 按 cwd 存放并查找会话记录，
	// 启动目录一变，已存的 session 就续不上（详见 claude.Options.WorkDir 注释）。
	bot := bridge.New(
		claude.NewClient(claude.Options{
			CLIPath: cfg.Claude.CLIPath,
			Timeout: cfg.Claude.Timeout(),
		}),
		feishu.NewClient(cfg.Feishu.AppID, cfg.Feishu.AppSecret),
	)

	// 3. 注册事件 Register event
	eventHandler := dispatcher.NewEventDispatcher("", "").
		OnP2MessageReceiveV1(bot.HandlerFeishuMsg)

	// 4. 构建 client Build client（凭证来自 .env，不落代码库）
	cli := larkws.NewClient(cfg.Feishu.AppID, cfg.Feishu.AppSecret,
		larkws.WithEventHandler(eventHandler),
		larkws.WithLogLevel(larkcore.LogLevelDebug),
	)

	// 5. 建立长连接 Establish persistent connection
	if err := cli.Start(context.Background()); err != nil {
		log.Fatalf("建立长连接失败: %v", err)
	}

	// 可选：Web 服务（数据库 + HTTP 路由），当前未启用
	// database.Init(cfg)
	// r := router.Init()
	// if err := r.Run(fmt.Sprintf(":%d", cfg.Server.Port)); err != nil {
	// 	panic(err)
	// }
}
