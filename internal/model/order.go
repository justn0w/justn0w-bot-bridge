package model

import "time"

// 订单状态常量
const (
	OrderStatusPending = 0 // 待支付
	OrderStatusPaid    = 1 // 已支付
	OrderStatusCancel  = 2 // 已取消
)

// Order 订单模型
type Order struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	OrderNo     string    `gorm:"type:varchar(64);uniqueIndex;not null;comment:订单号" json:"order_no"`
	UserID      uint      `gorm:"not null;index;comment:用户ID" json:"user_id"`
	ProductName string    `gorm:"type:varchar(255);not null;comment:商品名称" json:"product_name"`
	Amount      float64   `gorm:"type:decimal(10,2);not null;comment:订单金额" json:"amount"`
	Status      int       `gorm:"type:tinyint;default:0;comment:订单状态" json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TableName 指定表名
func (Order) TableName() string {
	return "orders"
}
