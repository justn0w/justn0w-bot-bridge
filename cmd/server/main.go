package main

import (
	"fmt"
	"log"

	"justn0w-bot-bridge/internal/config"
	"justn0w-bot-bridge/internal/database"
	"justn0w-bot-bridge/internal/router"
)

func main() {
	// 1. 加载配置
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 2. 初始化数据库并自动建表
	database.Init(cfg)

	// 3. 注册路由并启动服务
	r := router.Init()
	if err := r.Run(fmt.Sprintf(":%d", cfg.Server.Port)); err != nil {
		panic(err)
	}
}
