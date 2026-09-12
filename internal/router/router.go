package router

import (
	"net/http"

	"justn0w-bot-bridge/internal/handler"

	"github.com/gin-gonic/gin"
)

// Init 注册路由，返回 gin.Engine
func Init() *gin.Engine {
	r := gin.Default()

	// 健康检查
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	api := r.Group("/api/v1")
	{
		api.POST("/orders", handler.CreateOrder)     // 创建订单
		api.GET("/orders", handler.ListOrders)       // 订单列表
		api.GET("/orders/:id", handler.GetOrderByID) // 订单详情
	}

	return r
}
