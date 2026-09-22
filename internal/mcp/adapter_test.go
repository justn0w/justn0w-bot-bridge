package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

// fakeTool 是最小的 InvokableTool 实现。
// 用假的而不是真的订单工具，是为了把适配层的成败和业务逻辑隔离开：
// 适配层挂了就该在这一层报错，而不是等业务用例先红。
type fakeTool struct {
	name   string
	desc   string
	params *schema.ParamsOneOf // 允许为 nil，用于覆盖无入参工具
	out    string
	err    error

	// gotArgs 记录适配层实际传下来的入参 JSON
	gotArgs string
}

func (f *fakeTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: f.name, Desc: f.desc, ParamsOneOf: f.params}, nil
}

func (f *fakeTool) InvokableRun(_ context.Context, args string, _ ...tool.Option) (string, error) {
	f.gotArgs = args
	return f.out, f.err
}

// callReq 造一个 MCP 工具调用请求
func callReq(name string, args any) mcpgo.CallToolRequest {
	return mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{Name: name, Arguments: args},
	}
}

// resultText 取出结果里的第一段文本，顺带断言它确实是文本内容
func resultText(t *testing.T, r *mcpgo.CallToolResult) string {
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

func TestToServerToolMapsNameAndDescription(t *testing.T) {
	ft := &fakeTool{name: "demo_tool", desc: "演示用工具"}

	st, err := toServerTool(context.Background(), ft)
	if err != nil {
		t.Fatalf("toServerTool() = %v, want nil", err)
	}

	if st.Tool.Name != "demo_tool" {
		t.Errorf("Name = %q, want demo_tool", st.Tool.Name)
	}
	if st.Tool.Description != "演示用工具" {
		t.Errorf("Description = %q, want 演示用工具", st.Tool.Description)
	}
	if st.Handler == nil {
		t.Error("Handler = nil, want 非空")
	}
}

// TestToServerToolPassesSchemaThrough 确认 eino 推导出的 schema 被原样透传，
// 且展开的是可读的属性定义，不是被 MCP 结构化字段截断后的残骸。
func TestToServerToolPassesSchemaThrough(t *testing.T) {
	tools, err := OrderTools()
	if err != nil {
		t.Fatalf("OrderTools() = %v, want nil", err)
	}

	var target tool.InvokableTool
	for _, tl := range tools {
		info, err := tl.Info(context.Background())
		if err != nil {
			t.Fatalf("Info() = %v, want nil", err)
		}
		if info.Name == "list_orders" {
			target = tl
			break
		}
	}
	if target == nil {
		t.Fatal("没找到 list_orders 工具")
	}

	st, err := toServerTool(context.Background(), target)
	if err != nil {
		t.Fatalf("toServerTool() = %v, want nil", err)
	}
	if len(st.Tool.RawInputSchema) == 0 {
		t.Fatal("RawInputSchema 为空")
	}

	var got struct {
		Type       string                     `json:"type"`
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(st.Tool.RawInputSchema, &got); err != nil {
		t.Fatalf("RawInputSchema 不是合法 JSON: %v（原文 %s）", err, st.Tool.RawInputSchema)
	}

	if got.Type != "object" {
		t.Errorf("type = %q, want object", got.Type)
	}
	for _, key := range []string{"user_id", "page", "page_size"} {
		if _, ok := got.Properties[key]; !ok {
			t.Errorf("properties 缺少 %q，实际有 %v", key, keysOf(got.Properties))
		}
	}
	// user_id 标了 jsonschema:"required"，必须出现在 required 里——
	// 少了它模型会不带 user_id 就调用，然后撞上运行时的报错
	if !contains(got.Required, "user_id") {
		t.Errorf("required = %v, want 含 user_id", got.Required)
	}
}

func TestToServerToolWithoutParamsUsesEmptySchema(t *testing.T) {
	st, err := toServerTool(context.Background(), &fakeTool{name: "no_arg_tool"})
	if err != nil {
		t.Fatalf("toServerTool() = %v, want nil", err)
	}

	if string(st.Tool.RawInputSchema) != emptyInputSchema {
		t.Errorf("RawInputSchema = %s, want %s", st.Tool.RawInputSchema, emptyInputSchema)
	}
}

func TestCallHandlerReturnsToolOutput(t *testing.T) {
	ft := &fakeTool{name: "demo_tool", out: `{"ok":true}`}
	h := callHandler(ft, ft.name)

	res, err := h(context.Background(), callReq("demo_tool", map[string]any{"order_no": "ORD-TEST-1"}))
	if err != nil {
		t.Fatalf("handler = %v, want nil", err)
	}
	if res.IsError {
		t.Errorf("IsError = true, want false（正常返回不该被标成错误）")
	}
	if got := resultText(t, res); got != `{"ok":true}` {
		t.Errorf("文本 = %q, want 工具原样输出", got)
	}

	// 入参必须一路传到工具，且是 JSON 原文
	if !strings.Contains(ft.gotArgs, "ORD-TEST-1") {
		t.Errorf("工具收到入参 %q, want 含 ORD-TEST-1", ft.gotArgs)
	}
}

// TestCallHandlerSurfacesToolErrorAsResult 是这套适配最要紧的一条约定：
// 工具错误必须以 IsError 的「正常结果」返回，而不是 Go error。
// 返回 Go error 会被 MCP 当协议错误，模型只看到「调用失败」，
// 看不到「订单不存在」这种能自我纠正的信息。
func TestCallHandlerSurfacesToolErrorAsResult(t *testing.T) {
	ft := &fakeTool{name: "demo_tool", err: errors.New("订单不存在")}
	h := callHandler(ft, ft.name)

	res, err := h(context.Background(), callReq("demo_tool", map[string]any{}))
	if err != nil {
		t.Fatalf("handler 返回了 Go error %v, want nil（应包成结果内的错误）", err)
	}
	if !res.IsError {
		t.Fatal("IsError = false, want true")
	}

	text := resultText(t, res)
	if !strings.Contains(text, "订单不存在") {
		t.Errorf("文本 = %q, want 含原始错误原因", text)
	}
	// 带上工具名，多工具场景下才知道是谁挂了
	if !strings.Contains(text, "demo_tool") {
		t.Errorf("文本 = %q, want 含工具名 demo_tool", text)
	}
}

func TestCallHandlerInvalidArguments(t *testing.T) {
	ft := &fakeTool{name: "demo_tool", out: "never"}
	h := callHandler(ft, ft.name)

	// 函数值无法序列化成 JSON，用来模拟畸形入参
	res, err := h(context.Background(), callReq("demo_tool", map[string]any{"bad": func() {}}))
	if err != nil {
		t.Fatalf("handler = %v, want nil", err)
	}
	if !res.IsError {
		t.Fatal("IsError = false, want true（入参解析失败应报错）")
	}
	if ft.gotArgs != "" {
		t.Errorf("工具被调用了（入参 %q），want 不调用", ft.gotArgs)
	}
}

// TestCallHandlerWithNilArguments 覆盖模型不传参数的情况：
// 应还原成空对象而不是让工具收到 "null"。
func TestCallHandlerWithNilArguments(t *testing.T) {
	ft := &fakeTool{name: "demo_tool", out: "ok"}
	h := callHandler(ft, ft.name)

	if _, err := h(context.Background(), callReq("demo_tool", nil)); err != nil {
		t.Fatalf("handler = %v, want nil", err)
	}
	if ft.gotArgs != "{}" {
		t.Errorf("工具收到入参 %q, want {}", ft.gotArgs)
	}
}

func TestMarshalArguments(t *testing.T) {
	tests := []struct {
		name string
		args any
		want string
	}{
		{"nil 还原成空对象", nil, "{}"},
		{"map 原样序列化", map[string]any{"page": 1}, `{"page":1}`},
		{"字符串原样序列化", "ORD-1", `"ORD-1"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := marshalArguments(callReq("demo_tool", tt.args))
			if err != nil {
				t.Fatalf("marshalArguments() = %v, want nil", err)
			}
			if string(got) != tt.want {
				t.Errorf("marshalArguments() = %s, want %s", got, tt.want)
			}
		})
	}
}

// TestNewServer 走一遍真实装配：工具构造 + 注册。任何一步挂掉这里都会红。
func TestNewServer(t *testing.T) {
	srv, err := NewServer(context.Background())
	if err != nil {
		t.Fatalf("NewServer() = %v, want nil", err)
	}
	if srv == nil {
		t.Fatal("NewServer() = nil, want 非空 server")
	}
}

// keysOf 把 map 的键摊平，仅用于失败信息里展示实际有哪些属性
func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
