package main

import (
	"context"
	"log"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"

	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"

	"justn0w-bot-bridge/internal/bridge"
	"justn0w-bot-bridge/internal/config"
	"justn0w-bot-bridge/internal/feishu"
	"justn0w-bot-bridge/internal/llm"
)

func main() {
	// 1. 加载配置（.env + configs/config.yaml）
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 2. 组装答疑依赖：模型生成答案，飞书回发消息
	bot := bridge.New(
		llm.NewClient(llm.Options{
			APIKey:    cfg.LLM.APIKey,
			BaseURL:   cfg.LLM.BaseURL,
			Model:     cfg.LLM.Model,
			MaxTokens: cfg.LLM.MaxTokens,
			Timeout:   cfg.LLM.Timeout(),
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
