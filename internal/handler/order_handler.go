package handler

import (
	"net/http"
	"strconv"

	"justn0w-bot-bridge/internal/dto"
	"justn0w-bot-bridge/internal/service"
	"justn0w-bot-bridge/pkg/response"

	"github.com/gin-gonic/gin"
)

// CreateOrder POST /api/v1/orders 创建订单
func CreateOrder(c *gin.Context) {
	var req dto.CreateOrderReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}

	order, err := service.CreateOrder(req)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, order)
}

// GetOrderByID GET /api/v1/orders/:id 根据 ID 查询订单
func GetOrderByID(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "无效的订单ID")
		return
	}

	order, err := service.GetOrderByID(uint(id))
	if err != nil {
		response.Fail(c, http.StatusNotFound, err.Error())
		return
	}
	response.Success(c, order)
}

// ListOrders GET /api/v1/orders 分页查询订单列表
func ListOrders(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 10
	}

	orders, total, err := service.ListOrders(page, pageSize)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, gin.H{
		"list":      orders,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}
