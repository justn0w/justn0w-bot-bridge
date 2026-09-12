package test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"justn0w-bot-bridge/internal/database"
	"justn0w-bot-bridge/internal/model"
	"justn0w-bot-bridge/internal/router"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// newTestServer 构建基于内存 SQLite 的完整 HTTP 服务
func newTestServer(t *testing.T) *gin.Engine {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("初始化内存数据库失败: %v", err)
	}
	if err := db.AutoMigrate(&model.Order{}); err != nil {
		t.Fatalf("迁移表结构失败: %v", err)
	}

	// 注入内存数据库到全局 db 客户端
	database.SetDB(db)

	return router.Init()
}

func TestCreateAndGetOrder(t *testing.T) {
	r := newTestServer(t)

	// 1. 创建订单
	body := `{"user_id": 1, "product_name": "iPhone 15", "amount": 5999.00}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("创建订单状态码 = %d, body = %s", w.Code, w.Body.String())
	}

	var created struct {
		Data model.Order `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("解析创建响应失败: %v", err)
	}
	if created.Data.ID == 0 || created.Data.OrderNo == "" {
		t.Fatalf("创建订单返回异常: %+v", created.Data)
	}

	// 2. 按 ID 查询
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/orders/%d", created.Data.ID), nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("查询订单状态码 = %d", w.Code)
	}

	// 3. 分页列表查询
	req = httptest.NewRequest(http.MethodGet, "/api/v1/orders?page=1&page_size=10", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("列表查询状态码 = %d", w.Code)
	}

	var listResp struct {
		Data struct {
			Total int64 `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("解析列表响应失败: %v", err)
	}
	if listResp.Data.Total != 1 {
		t.Fatalf("列表 total = %d, want 1", listResp.Data.Total)
	}
}

func TestCreateOrderValidation(t *testing.T) {
	r := newTestServer(t)

	// 缺少必填字段 / 金额非法，应返回 400
	body := `{"user_id": 1, "product_name": "", "amount": 0}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("参数校验状态码 = %d, want %d", w.Code, http.StatusBadRequest)
	}
}
