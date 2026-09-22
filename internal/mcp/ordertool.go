package mcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"justn0w-bot-bridge/internal/model"
	"justn0w-bot-bridge/internal/service"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

// 分页参数的合法区间。入参由模型生成，属于不可信输入，
// 收口在这里，避免非法页码变成 SQL 上的负 offset。
const (
	defaultPage     = 1
	defaultPageSize = 10
	maxPageSize     = 50
)

// getOrderArgs get_order 的入参，订单号与 ID 二选一。
type getOrderArgs struct {
	OrderNo string `json:"order_no" jsonschema_description:"订单号，例如 ORD202409010001。与 id 二选一，两者都传时以订单号为准"`
	ID      uint   `json:"id" jsonschema_description:"订单主键 ID。与 order_no 二选一"`
}

// listOrdersArgs list_orders 的入参。
type listOrdersArgs struct {
	UserID   uint `json:"user_id" jsonschema:"required" jsonschema_description:"用户 ID，必填"`
	Page     int  `json:"page" jsonschema_description:"页码，从 1 开始，默认 1"`
	PageSize int  `json:"page_size" jsonschema_description:"每页条数，默认 10，最大 50"`
}

// orderView 对外暴露的订单视图。
//
// 刻意不复用 model.Order：一来状态在库里是 0/1/2，直接给模型看数字它判断不出含义；
// 二来数据库结构变化不应该直接冲击 MCP 的对外契约。
type orderView struct {
	OrderNo     string  `json:"order_no"`
	UserID      uint    `json:"user_id"`
	ProductName string  `json:"product_name"`
	Amount      float64 `json:"amount"`
	Status      string  `json:"status"`
	CreatedAt   string  `json:"created_at"`
}

// listOrdersResult list_orders 的出参，带总数便于模型判断是否还有下一页。
type listOrdersResult struct {
	Total    int64       `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"page_size"`
	Orders   []orderView `json:"orders"`
}

// OrderTools 用 eino 的 InferTool 构造订单查询工具。
//
// InferTool 依据入参结构体的字段与 tag 自动推导 JSON Schema，
// 因此参数定义只需维护结构体一处，MCP 与 eino 两侧自动保持一致。
func OrderTools() ([]tool.InvokableTool, error) {
	getOrderTool, err := utils.InferTool(
		"get_order",
		"根据订单号或订单 ID 查询单个订单的详情，返回商品名、金额、订单状态与创建时间。订单号与 ID 二选一。",
		getOrder,
	)
	if err != nil {
		return nil, fmt.Errorf("构造 get_order 工具失败: %w", err)
	}

	listOrdersTool, err := utils.InferTool(
		"list_orders",
		"分页查询指定用户的订单列表，返回订单列表与总数。必须提供用户 ID。",
		listOrders,
	)
	if err != nil {
		return nil, fmt.Errorf("构造 list_orders 工具失败: %w", err)
	}

	return []tool.InvokableTool{getOrderTool, listOrdersTool}, nil
}

// getOrder 按订单号或 ID 查询单个订单
func getOrder(_ context.Context, args *getOrderArgs) (*orderView, error) {
	var (
		order *model.Order
		err   error
	)

	switch {
	case args.OrderNo != "":
		order, err = service.GetOrderByOrderNo(args.OrderNo)
	case args.ID != 0:
		order, err = service.GetOrderByID(args.ID)
	default:
		return nil, errors.New("必须提供 order_no 或 id 之一")
	}
	if err != nil {
		return nil, err
	}

	view := toView(order)
	return &view, nil
}

// listOrders 分页查询指定用户的订单
func listOrders(_ context.Context, args *listOrdersArgs) (*listOrdersResult, error) {
	if args.UserID == 0 {
		return nil, errors.New("user_id 不能为空")
	}

	page, pageSize := normalizePaging(args.Page, args.PageSize)
	orders, total, err := service.ListOrdersByUser(args.UserID, page, pageSize)
	if err != nil {
		return nil, err
	}

	views := make([]orderView, 0, len(orders))
	for i := range orders {
		views = append(views, toView(&orders[i]))
	}

	return &listOrdersResult{
		Total:    total,
		Page:     page,
		PageSize: pageSize,
		Orders:   views,
	}, nil
}

// normalizePaging 把非法分页参数收敛到合法区间
func normalizePaging(page, pageSize int) (int, int) {
	if page < 1 {
		page = defaultPage
	}

	switch {
	case pageSize < 1:
		pageSize = defaultPageSize
	case pageSize > maxPageSize:
		pageSize = maxPageSize
	}
	return page, pageSize
}

// toView 把数据库模型转成对外视图
func toView(o *model.Order) orderView {
	return orderView{
		OrderNo:     o.OrderNo,
		UserID:      o.UserID,
		ProductName: o.ProductName,
		Amount:      o.Amount,
		Status:      statusText(o.Status),
		CreatedAt:   o.CreatedAt.Format(time.DateTime),
	}
}

// statusText 把订单状态码翻译成模型能直接理解的中文
func statusText(status int) string {
	switch status {
	case model.OrderStatusPending:
		return "待支付"
	case model.OrderStatusPaid:
		return "已支付"
	case model.OrderStatusCancel:
		return "已取消"
	default:
		return fmt.Sprintf("未知状态(%d)", status)
	}
}
