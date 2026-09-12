package repository

import (
	"justn0w-bot-bridge/internal/database"
	"justn0w-bot-bridge/internal/model"
)

// CreateOrder 写入订单
func CreateOrder(order *model.Order) error {
	return database.GetDB().Create(order).Error
}

// GetOrderByID 根据主键查询订单
func GetOrderByID(id uint) (*model.Order, error) {
	var order model.Order
	if err := database.GetDB().First(&order, id).Error; err != nil {
		return nil, err
	}
	return &order, nil
}

// GetOrderByOrderNo 根据订单号查询订单
func GetOrderByOrderNo(orderNo string) (*model.Order, error) {
	var order model.Order
	if err := database.GetDB().Where("order_no = ?", orderNo).First(&order).Error; err != nil {
		return nil, err
	}
	return &order, nil
}

// ListOrders 分页查询订单列表，返回列表与总数
func ListOrders(page, pageSize int) ([]model.Order, int64, error) {
	db := database.GetDB()

	var orders []model.Order
	var total int64
	if err := db.Model(&model.Order{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	if err := db.Order("id DESC").Limit(pageSize).Offset(offset).Find(&orders).Error; err != nil {
		return nil, 0, err
	}
	return orders, total, nil
}
