// Package mcp 把 eino 的工具能力封装为 MCP(Model Context Protocol)服务。
//
// 设计取舍：eino-ext 只提供了 MCP 的客户端能力（把远端 MCP 工具转成 eino tool），
// 服务端需要自行搭建。这里让工具本身保持 eino 的 InvokableTool 形态——
// 既能注册进 MCP 供外部客户端（如 Claude Code）调用，
// 也能直接交给 eino 的 ChatModel / Agent 本地调用——
// 再用一层薄适配把 eino 的 ToolInfo 映射成 MCP 的工具描述。
//
// 这样参数定义只在入参结构体上维护一处，两侧的 schema 不会各自漂移。
package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// emptyInputSchema 供无入参工具兜底。
// MCP 客户端普遍要求 inputSchema.type 存在，缺省会给一个合法的空对象 schema。
const emptyInputSchema = `{"type":"object","properties":{}}`

// toServerTool 把 eino 的 InvokableTool 适配成 MCP 的 ServerTool。
func toServerTool(ctx context.Context, t tool.InvokableTool) (server.ServerTool, error) {
	info, err := t.Info(ctx)
	if err != nil {
		return server.ServerTool{}, fmt.Errorf("读取工具元信息失败: %w", err)
	}

	rawSchema, err := inputSchema(info)
	if err != nil {
		return server.ServerTool{}, err
	}

	return server.ServerTool{
		Tool: mcpgo.Tool{
			Name:        info.Name,
			Description: info.Desc,
			// 用 RawInputSchema 原样透传 eino 生成的 JSON Schema。
			// 换成结构化的 InputSchema 会把 Schema 收窄成 MCP 预设的几个字段，
			// 丢掉 $defs、anyOf 这类表达能力。两者互斥，同设会被 MCP 拒绝。
			RawInputSchema: rawSchema,
		},
		Handler: callHandler(t, info.Name),
	}, nil
}

// inputSchema 把 eino 的入参定义序列化成 JSON Schema 字节。
func inputSchema(info *schema.ToolInfo) (json.RawMessage, error) {
	if info.ParamsOneOf == nil {
		// 无入参工具，给一个合法的空对象 schema，避免客户端解析失败
		return json.RawMessage(emptyInputSchema), nil
	}

	s, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		return nil, fmt.Errorf("工具 %s 的入参 schema 转换失败: %w", info.Name, err)
	}

	b, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("序列化工具 %s 的入参 schema 失败: %w", info.Name, err)
	}
	return b, nil
}

// callHandler 把 MCP 的调用请求转成 eino 工具的调用。
//
// 工具内部的错误以 CallToolResult.IsError 返回，而不是 Go error：
// handler 返回 Go error 会被 MCP 当成 JSON-RPC 协议错误，
// 模型只能看到「调用失败」；而 IsError 能把「订单不存在」这类
// 具体原因带回给模型，让它有机会自我纠正。
func callHandler(t tool.InvokableTool, name string) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args, err := marshalArguments(req)
		if err != nil {
			return mcpgo.NewToolResultErrorf("工具 %s 入参解析失败: %v", name, err), nil
		}

		out, err := t.InvokableRun(ctx, string(args))
		if err != nil {
			return mcpgo.NewToolResultErrorf("工具 %s 执行失败: %v", name, err), nil
		}
		return mcpgo.NewToolResultText(out), nil
	}
}

// marshalArguments 把 MCP 请求里的参数还原成 eino 需要的 JSON 字符串。
// 走 GetRawArguments 而不是 GetArguments：后者在参数不是 map 时直接返回 nil，
// 会静默丢掉入参。
func marshalArguments(req mcpgo.CallToolRequest) ([]byte, error) {
	raw := req.GetRawArguments()
	if raw == nil {
		return []byte("{}"), nil
	}

	b, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("参数不是合法 JSON: %w", err)
	}
	return b, nil
}
