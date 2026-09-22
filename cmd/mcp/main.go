// Command mcp 以 stdio 方式提供订单查询的 MCP 服务。
//
// 接入 Claude Code：
//
//	make mcp
//	claude mcp add order-bot -- ./bin/mcp
//
// 与 cmd/server 的区别：那个是飞书机器人（建长连接、常驻），
// 这个是 MCP 服务端（由客户端拉起、随客户端退出），两者互不影响。
package main

import (
	"context"
	"log"
	"os"

	"justn0w-bot-bridge/internal/config"
	"justn0w-bot-bridge/internal/database"
	"justn0w-bot-bridge/internal/mcp"
)

func main() {
	// stdio 模式下 stdout 是 JSON-RPC 通道，任何一行多余的输出都会让客户端解析失败。
	// log 默认写 stderr，这里显式固定一次，避免日后有人改了全局默认值踩坑。
	log.SetOutput(os.Stderr)

	// 只依赖数据库配置：订单查询用不到飞书与答疑模型，
	// 强制校验那两处凭证会让 MCP 在没配凭证的机器上直接起不来。
	cfg, err := config.Load(config.WithoutCredentialCheck())
	if err != nil {
		// 配置按 CWD 下的 ./configs、. 查找，被客户端拉起时 CWD 不对就会走到这里
		log.Fatalf("加载配置失败（当前目录 %s，请确认其下有 configs/config.yaml）: %v", mustGetwd(), err)
	}

	database.Init(cfg)

	if err := mcp.Serve(context.Background()); err != nil {
		log.Fatalf("订单 MCP 服务退出: %v", err)
	}
}

// mustGetwd 取当前工作目录，仅供错误提示使用，取不到就返回占位符
func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "(未知)"
	}
	return wd
}
