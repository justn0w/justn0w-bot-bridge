package mcp

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/server"
)

// serverName / serverVersion 会在 MCP 握手时告知客户端
const (
	serverName    = "justn0w-order-mcp"
	serverVersion = "0.1.0"
)

// NewServer 组装 MCP server，把订单工具注册进来。
func NewServer(ctx context.Context) (*server.MCPServer, error) {
	tools, err := OrderTools()
	if err != nil {
		return nil, err
	}

	// 工具集在启动时固定，不会动态增删，所以 listChanged 传 false
	srv := server.NewMCPServer(serverName, serverVersion, server.WithToolCapabilities(false))

	for _, t := range tools {
		st, err := toServerTool(ctx, t)
		if err != nil {
			return nil, err
		}
		srv.AddTool(st.Tool, st.Handler)
	}

	return srv, nil
}

// Serve 在 stdio 上提供 MCP 服务，阻塞直到客户端关闭连接。
//
// 走 stdio 时 stdout 是 JSON-RPC 通道，任何调试输出都必须写 stderr，
// 否则会污染协议流导致客户端解析失败。日志统一走 log（默认输出到 stderr）。
func Serve(ctx context.Context) error {
	srv, err := NewServer(ctx)
	if err != nil {
		return err
	}

	if err := server.ServeStdio(srv); err != nil {
		return fmt.Errorf("MCP stdio 服务异常退出: %w", err)
	}
	return nil
}
