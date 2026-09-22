package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	"justn0w-bot-bridge/internal/database"
	"justn0w-bot-bridge/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// newTestDB 用内存 SQLite 装配订单表并注入全局 db 客户端。
// 与 test/integration_test.go 保持同一套做法，避免两处各写一份。
func newTestDB(t *testing.T) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("初始化内存数据库失败: %v", err)
	}
	if err := db.AutoMigrate(&model.Order{}); err != nil {
		t.Fatalf("迁移表结构失败: %v", err)
	}
	database.SetDB(db)
}

// seedOrders 造固定数据：用户 1001 三笔、用户 1002 一笔。
// 两个用户是必要的——只造一个用户时，「按用户过滤」写不写 WHERE 都能过，
// 用例会假通过。
func seedOrders(t *testing.T) {
	t.Helper()

	orders := []model.Order{
		{OrderNo: "ORD-A-1", UserID: 1001, ProductName: "iPhone 15", Amount: 5999, Status: model.OrderStatusPending},
		{OrderNo: "ORD-A-2", UserID: 1001, ProductName: "AirPods", Amount: 1299, Status: model.OrderStatusPaid},
		{OrderNo: "ORD-A-3", UserID: 1001, ProductName: "iPad", Amount: 4599, Status: model.OrderStatusCancel},
		{OrderNo: "ORD-B-1", UserID: 1002, ProductName: "MacBook", Amount: 15999, Status: model.OrderStatusPaid},
	}
	for i := range orders {
		if err := database.GetDB().Create(&orders[i]).Error; err != nil {
			t.Fatalf("造订单 %s 失败: %v", orders[i].OrderNo, err)
		}
	}
}

func TestGetOrderByOrderNo(t *testing.T) {
	newTestDB(t)
	seedOrders(t)

	got, err := getOrder(context.Background(), &getOrderArgs{OrderNo: "ORD-A-2"})
	if err != nil {
		t.Fatalf("getOrder() = %v, want nil", err)
	}

	if got.OrderNo != "ORD-A-2" || got.ProductName != "AirPods" || got.Amount != 1299 {
		t.Errorf("订单 = %+v, want ORD-A-2/AirPods/1299", got)
	}
	// 状态以中文回给模型，0/1/2 对模型没有意义
	if got.Status != "已支付" {
		t.Errorf("Status = %q, want %q", got.Status, "已支付")
	}
}

func TestGetOrderByID(t *testing.T) {
	newTestDB(t)
	seedOrders(t)

	// 先按订单号取到主键，再按主键查，避免把自增 ID 硬编码进用例
	first, err := getOrder(context.Background(), &getOrderArgs{OrderNo: "ORD-B-1"})
	if err != nil {
		t.Fatalf("先按订单号查询失败: %v", err)
	}

	var row model.Order
	if err := database.GetDB().Where("order_no = ?", first.OrderNo).First(&row).Error; err != nil {
		t.Fatalf("取主键失败: %v", err)
	}

	got, err := getOrder(context.Background(), &getOrderArgs{ID: row.ID})
	if err != nil {
		t.Fatalf("getOrder() = %v, want nil", err)
	}
	if got.OrderNo != "ORD-B-1" {
		t.Errorf("订单号 = %q, want ORD-B-1", got.OrderNo)
	}
}

// TestGetOrderPrefersOrderNo 确认两者都传时以订单号为准（工具描述里的承诺）
func TestGetOrderPrefersOrderNo(t *testing.T) {
	newTestDB(t)
	seedOrders(t)

	// id 故意给一个不存在的值：若实现改成优先用 id，这里会查空
	got, err := getOrder(context.Background(), &getOrderArgs{OrderNo: "ORD-A-1", ID: 999999})
	if err != nil {
		t.Fatalf("getOrder() = %v, want nil", err)
	}
	if got.OrderNo != "ORD-A-1" {
		t.Errorf("订单号 = %q, want ORD-A-1", got.OrderNo)
	}
}

func TestGetOrderWithoutAnyKey(t *testing.T) {
	newTestDB(t)

	if _, err := getOrder(context.Background(), &getOrderArgs{}); err == nil {
		t.Error("getOrder() = nil, want error（两个键都没传时应报错）")
	}
}

func TestGetOrderNotFound(t *testing.T) {
	newTestDB(t)
	seedOrders(t)

	_, err := getOrder(context.Background(), &getOrderArgs{OrderNo: "ORD-NOT-EXIST"})
	if err == nil {
		t.Fatal("getOrder() = nil, want error")
	}
	// service 层把 gorm 的 RecordNotFound 翻成了可读文案，这层不该把它吞掉
	if !strings.Contains(err.Error(), "订单不存在") {
		t.Errorf("err = %v, want 含「订单不存在」", err)
	}
}

func TestListOrdersFiltersByUser(t *testing.T) {
	newTestDB(t)
	seedOrders(t)

	got, err := listOrders(context.Background(), &listOrdersArgs{UserID: 1001})
	if err != nil {
		t.Fatalf("listOrders() = %v, want nil", err)
	}

	if got.Total != 3 {
		t.Errorf("Total = %d, want 3", got.Total)
	}
	if len(got.Orders) != 3 {
		t.Fatalf("订单数 = %d, want 3", len(got.Orders))
	}
	// 用户 1002 的订单不得出现
	for _, o := range got.Orders {
		if o.UserID != 1001 {
			t.Errorf("订单 %s 的 user_id = %d, want 1001", o.OrderNo, o.UserID)
		}
	}
}

func TestListOrdersPaging(t *testing.T) {
	newTestDB(t)
	seedOrders(t)

	page1, err := listOrders(context.Background(), &listOrdersArgs{UserID: 1001, Page: 1, PageSize: 2})
	if err != nil {
		t.Fatalf("第 1 页失败: %v", err)
	}
	if len(page1.Orders) != 2 {
		t.Errorf("第 1 页 = %d 条, want 2", len(page1.Orders))
	}
	// total 是命中总数而非当页条数，模型据此判断还有没有下一页
	if page1.Total != 3 {
		t.Errorf("第 1 页 Total = %d, want 3", page1.Total)
	}

	page2, err := listOrders(context.Background(), &listOrdersArgs{UserID: 1001, Page: 2, PageSize: 2})
	if err != nil {
		t.Fatalf("第 2 页失败: %v", err)
	}
	if len(page2.Orders) != 1 {
		t.Errorf("第 2 页 = %d 条, want 1", len(page2.Orders))
	}
	if page2.Orders[0].OrderNo == page1.Orders[0].OrderNo {
		t.Error("第 2 页与第 1 页返回了同一条订单，分页未生效")
	}
}

// TestListOrdersNoData 确认查无数据时返回空列表而不是错误，
// 且 orders 不是 null——模型看到 null 容易误判成调用失败。
func TestListOrdersNoData(t *testing.T) {
	newTestDB(t)
	seedOrders(t)

	got, err := listOrders(context.Background(), &listOrdersArgs{UserID: 88888})
	if err != nil {
		t.Fatalf("listOrders() = %v, want nil", err)
	}
	if got.Total != 0 || len(got.Orders) != 0 {
		t.Errorf("结果 = %+v, want 空", got)
	}
	if got.Orders == nil {
		t.Error("Orders = nil, want 非 nil 空切片")
	}
}

func TestListOrdersRequiresUserID(t *testing.T) {
	newTestDB(t)

	if _, err := listOrders(context.Background(), &listOrdersArgs{}); err == nil {
		t.Error("listOrders() = nil, want error（user_id 为空时应报错）")
	}
}

// TestNormalizePaging 覆盖模型给过来的非法分页参数。
// 这些值属于不可信输入，放行会让第 1 页变成负 offset。
func TestNormalizePaging(t *testing.T) {
	tests := []struct {
		name         string
		page         int
		pageSize     int
		wantPage     int
		wantPageSize int
	}{
		{"零值取默认", 0, 0, defaultPage, defaultPageSize},
		{"负数取默认", -3, -10, defaultPage, defaultPageSize},
		{"正常值原样保留", 2, 20, 2, 20},
		{"超过上限被截断", 1, 1000, 1, maxPageSize},
		{"刚好等于上限不截断", 1, maxPageSize, 1, maxPageSize},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPage, gotSize := normalizePaging(tt.page, tt.pageSize)
			if gotPage != tt.wantPage || gotSize != tt.wantPageSize {
				t.Errorf("normalizePaging(%d, %d) = (%d, %d), want (%d, %d)",
					tt.page, tt.pageSize, gotPage, gotSize, tt.wantPage, tt.wantPageSize)
			}
		})
	}
}

// TestListOrdersClampsPageSize 确认超限的 page_size 真的被收口，
// 而不是只在 normalizePaging 里对——后者单独过不代表调用链上生效
func TestListOrdersClampsPageSize(t *testing.T) {
	newTestDB(t)
	seedOrders(t)

	got, err := listOrders(context.Background(), &listOrdersArgs{UserID: 1001, PageSize: 1000})
	if err != nil {
		t.Fatalf("listOrders() = %v, want nil", err)
	}
	if got.PageSize != maxPageSize {
		t.Errorf("PageSize = %d, want %d", got.PageSize, maxPageSize)
	}
}

func TestStatusText(t *testing.T) {
	tests := []struct {
		status int
		want   string
	}{
		{model.OrderStatusPending, "待支付"},
		{model.OrderStatusPaid, "已支付"},
		{model.OrderStatusCancel, "已取消"},
		{99, "未知状态(99)"},
	}

	for _, tt := range tests {
		if got := statusText(tt.status); got != tt.want {
			t.Errorf("statusText(%d) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

// TestToViewFormatsCreatedAt 用固定时间断言对外的时间格式，
// 不依赖数据库写回，避免时区差异让用例飘。
func TestToViewFormatsCreatedAt(t *testing.T) {
	created := time.Date(2024, 9, 1, 10, 30, 0, 0, time.Local)

	got := toView(&model.Order{
		OrderNo:   "ORD-X-1",
		UserID:    7,
		Amount:    12.5,
		Status:    model.OrderStatusPaid,
		CreatedAt: created,
	})

	if got.CreatedAt != "2024-09-01 10:30:00" {
		t.Errorf("CreatedAt = %q, want %q", got.CreatedAt, "2024-09-01 10:30:00")
	}
	if got.Status != "已支付" {
		t.Errorf("Status = %q, want 已支付", got.Status)
	}
}

// TestOrderToolsAreInvokable 走一遍 OrderTools 的真实构造路径：
// InferTool 若因 tag 写错而失败，或工具名重复，会在这里暴露。
func TestOrderToolsAreInvokable(t *testing.T) {
	newTestDB(t)
	seedOrders(t)

	tools, err := OrderTools()
	if err != nil {
		t.Fatalf("OrderTools() = %v, want nil", err)
	}
	if len(tools) != 2 {
		t.Fatalf("工具数 = %d, want 2", len(tools))
	}

	byName := make(map[string]bool, len(tools))
	for _, tl := range tools {
		info, err := tl.Info(context.Background())
		if err != nil {
			t.Fatalf("Info() = %v, want nil", err)
		}
		if info.Name == "" || info.Desc == "" {
			t.Errorf("工具 %+v 的名称或描述为空", info)
		}
		if byName[info.Name] {
			t.Errorf("工具名 %q 重复", info.Name)
		}
		byName[info.Name] = true
	}

	for _, want := range []string{"get_order", "list_orders"} {
		if !byName[want] {
			t.Errorf("缺少工具 %q", want)
		}
	}

	// 以 JSON 入参调用，验证 InferTool 的解码链路真的通到业务函数
	for _, tl := range tools {
		info, _ := tl.Info(context.Background())
		if info.Name != "get_order" {
			continue
		}
		out, err := tl.InvokableRun(context.Background(), `{"order_no":"ORD-A-1"}`)
		if err != nil {
			t.Fatalf("InvokableRun() = %v, want nil", err)
		}
		if !strings.Contains(out, "ORD-A-1") {
			t.Errorf("结果 = %q, want 含 ORD-A-1", out)
		}
	}
}
