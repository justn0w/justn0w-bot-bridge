package main

import (
	"context"
	"fmt"
	"log"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"

	"justn0w-bot-bridge/internal/config"
)

func main() {
	// 1. 加载配置（.env + configs/config.yaml）
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 2. 注册事件 Register event
	eventHandler := dispatcher.NewEventDispatcher("", "").
		OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
			fmt.Printf("[ OnP2MessageReceiveV1 access ], data: %s\n", larkcore.Prettify(event))
			return nil
		})

	// 3. 构建 client Build client（凭证来自 .env，不落代码库）
	cli := larkws.NewClient(cfg.Feishu.AppID, cfg.Feishu.AppSecret,
		larkws.WithEventHandler(eventHandler),
		larkws.WithLogLevel(larkcore.LogLevelDebug),
	)

	// 4. 建立长连接 Establish persistent connection
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
