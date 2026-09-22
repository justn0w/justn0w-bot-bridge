package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

// 本文件走的是真实 JSON-RPC：请求经 mcp-go 的传输层编解码后打进 server.HandleMessage，
// 与 cmd/mcp 跑 stdio 时经过的是同一条路径，只是把管道换成了内存。
//
// 为什么不直接调 adapter / ordertool：那两边已有用例，但它们都绕过了「工具注册表」。
// 一个工具若没被正确注册（名字写错、注册时被跳过），单独调它是过的，客户端却根本看不见。
// 这里从 Initialize 起，覆盖到 tools/list 与 tools/call，才算验证了「能被客户端用起来」。

// newTestClient 装配 server 并完成 MCP 握手，返回已初始化的客户端
func newTestClient(t *testing.T) *client.Client {
	t.Helper()

	srv, err := NewServer(context.Background())
	if err != nil {
		t.Fatalf("NewServer() = %v, want nil", err)
	}

	c, err := client.NewInProcessClient(srv)
	if err != nil {
		t.Fatalf("NewInProcessClient() = %v, want nil", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start() = %v, want nil", err)
	}

	var initReq mcpgo.InitializeRequest
	initReq.Params.ProtocolVersion = mcpgo.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcpgo.Implementation{Name: "订单 MCP 测试客户端", Version: "1.0.0"}

	res, err := c.Initialize(ctx, initReq)
	if err != nil {
		t.Fatalf("Initialize() = %v, want nil", err)
	}
	if res.ServerInfo.Name == "" {
		t.Error("握手结果里服务名为空")
	}
	if res.Capabilities.Tools == nil {
		t.Error("服务端未声明 tools 能力，客户端不会去拉工具列表")
	}
	return c
}

// toolText 从工具调用结果里取出第一段文本，顺带断言它确实是文本内容
func toolText(t *testing.T, r *mcpgo.CallToolResult) string {
	t.Helper()

	if len(r.Content) == 0 {
		t.Fatal("结果里没有任何内容")
	}
	tc, ok := r.Content[0].(mcpgo.TextContent)
	if !ok {
		t.Fatalf("内容类型 = %T, want TextContent", r.Content[0])
	}
	return tc.Text
}

// callTool 以 JSON 原文发起一次工具调用
func callTool(t *testing.T, c *client.Client, name, argsJSON string) *mcpgo.CallToolResult {
	t.Helper()

	var req mcpgo.CallToolRequest
	req.Params.Name = name
	req.Params.Arguments = json.RawMessage(argsJSON)

	res, err := c.CallTool(context.Background(), req)
	if err != nil {
		t.Fatalf("CallTool(%s) = %v, want nil", name, err)
	}
	return res
}

// TestProtocolListsRegisteredTools 确认两个订单工具真的注册进了服务端，
// 且带出去的 schema 里 user_id 是必填——模型据此才知道不传会被拒。
func TestProtocolListsRegisteredTools(t *testing.T) {
	newTestDB(t)
	seedOrders(t)

	c := newTestClient(t)

	res, err := c.ListTools(context.Background(), mcpgo.ListToolsRequest{})
	if err != nil {
		t.Fatalf("ListTools() = %v, want nil", err)
	}
	if len(res.Tools) != 2 {
		t.Fatalf("工具数 = %d, want 2（实际 %v）", len(res.Tools), keysOfTools(res.Tools))
	}

	byName := make(map[string]mcpgo.Tool, len(res.Tools))
	for _, tl := range res.Tools {
		if tl.Description == "" {
			t.Errorf("工具 %q 没有描述，模型无从判断何时调用", tl.Name)
		}
		byName[tl.Name] = tl
	}

	list, ok := byName["list_orders"]
	if !ok {
		t.Fatalf("工具列表里没有 list_orders，实际有 %v", keysOfTools(res.Tools))
	}
	if list.InputSchema.Type != "object" {
		t.Errorf("list_orders 的 schema type = %q, want object", list.InputSchema.Type)
	}
	for _, key := range []string{"user_id", "page", "page_size"} {
		if _, ok := list.InputSchema.Properties[key]; !ok {
			t.Errorf("list_orders 的 schema 缺少属性 %q", key)
		}
	}
	if !contains(list.InputSchema.Required, "user_id") {
		t.Errorf("list_orders 的 required = %v, want 含 user_id", list.InputSchema.Required)
	}
}

// TestProtocolGetOrder 覆盖正常查询：工具能查到数据，且结果不被标成错误
func TestProtocolGetOrder(t *testing.T) {
	newTestDB(t)
	seedOrders(t)

	c := newTestClient(t)
	res := callTool(t, c, "get_order", `{"order_no":"ORD-A-2"}`)

	if res.IsError {
		t.Fatalf("IsError = true, want false（文本 %q）", toolText(t, res))
	}

	text := toolText(t, res)
	for _, want := range []string{"ORD-A-2", "AirPods", "已支付"} {
		if !strings.Contains(text, want) {
			t.Errorf("结果 = %q, want 含 %q", text, want)
		}
	}
}

// TestProtocolGetOrderNotFound 是最该走一遍网络的一例：
// 「订单不存在」必须以 IsError 的结果回给模型，而不是 JSON-RPC 协议错误，
// 否则模型只看到「调用失败」，编不出「换个订单号再试」这种纠正动作。
func TestProtocolGetOrderNotFound(t *testing.T) {
	newTestDB(t)
	seedOrders(t)

	c := newTestClient(t)
	res := callTool(t, c, "get_order", `{"order_no":"ORD-NOT-EXIST"}`)

	if !res.IsError {
		t.Fatal("IsError = false, want true")
	}
	if text := toolText(t, res); !strings.Contains(text, "订单不存在") {
		t.Errorf("结果 = %q, want 含原始错误原因「订单不存在」", text)
	}
}

// TestProtocolListOrdersFilterByUser 确认客户端传下来的 user_id 真的落到了查询条件上
func TestProtocolListOrdersFilterByUser(t *testing.T) {
	newTestDB(t)
	seedOrders(t)

	c := newTestClient(t)
	res := callTool(t, c, "list_orders", `{"user_id":1001}`)

	if res.IsError {
		t.Fatalf("IsError = true, want false（文本 %q）", toolText(t, res))
	}

	text := toolText(t, res)
	if !strings.Contains(text, "ORD-A-1") {
		t.Errorf("结果 = %q, want 含用户 1001 的订单", text)
	}
	// 用户 1002 的订单不得越权出现在结果里
	if strings.Contains(text, "ORD-B-1") {
		t.Errorf("结果 = %q, 不该出现其他用户的订单 ORD-B-1", text)
	}
}

// TestProtocolUnknownTool 确认调不存在的工具会被服务端拒绝，
// 而不是被静默当成空结果——静默会让模型以为「查到了，就是没数据」
func TestProtocolUnknownTool(t *testing.T) {
	newTestDB(t)

	c := newTestClient(t)

	var req mcpgo.CallToolRequest
	req.Params.Name = "no_such_tool"
	req.Params.Arguments = map[string]any{}

	if _, err := c.CallTool(context.Background(), req); err == nil {
		t.Error("调用未注册的工具返回 nil, want error")
	}
}

// TestProtocolBadArguments 覆盖模型传了非法入参的情形：
// user_id 是数值字段，传中文描述应当被拒，而不是被静默当成 0 去查全表
func TestProtocolBadArguments(t *testing.T) {
	newTestDB(t)
	seedOrders(t)

	c := newTestClient(t)
	res := callTool(t, c, "list_orders", `{"user_id":"不是数字"}`)

	if !res.IsError {
		t.Fatalf("IsError = false, want true（文本 %q）", toolText(t, res))
	}
}

// keysOfTools 摊平工具名，仅用于失败信息里展示实际注册了哪些
func keysOfTools(tools []mcpgo.Tool) []string {
	out := make([]string, 0, len(tools))
	for _, tl := range tools {
		out = append(out, tl.Name)
	}
	return out
}
