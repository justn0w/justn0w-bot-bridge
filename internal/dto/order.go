// Package dto 定义接口层与业务层之间传递的数据传输对象（入参/出参）。
package dto

// CreateOrderReq 创建订单入参
type CreateOrderReq struct {
	UserID      uint    `json:"user_id" binding:"required"`
	ProductName string  `json:"product_name" binding:"required"`
	Amount      float64 `json:"amount" binding:"required,gt=0"`
}
