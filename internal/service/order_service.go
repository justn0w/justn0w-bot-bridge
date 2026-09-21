package service

import (
	"errors"
	"fmt"
	"time"

	"justn0w-bot-bridge/internal/dto"
	"justn0w-bot-bridge/internal/model"
	"justn0w-bot-bridge/internal/repository"

	"gorm.io/gorm"
)

// CreateOrder 创建订单，自动生成订单号
func CreateOrder(req dto.CreateOrderReq) (*model.Order, error) {
	order := &model.Order{
		OrderNo:     generateOrderNo(),
		UserID:      req.UserID,
		ProductName: req.ProductName,
		Amount:      req.Amount,
		Status:      model.OrderStatusPending,
	}
	if err := repository.CreateOrder(order); err != nil {
		return nil, err
	}
	return order, nil
}

// GetOrderByID 根据 ID 查询订单
func GetOrderByID(id uint) (*model.Order, error) {
	order, err := repository.GetOrderByID(id)
	if err != nil {
		return nil, wrapNotFound(err)
	}
	return order, nil
}

// GetOrderByOrderNo 根据订单号查询订单
func GetOrderByOrderNo(orderNo string) (*model.Order, error) {
	order, err := repository.GetOrderByOrderNo(orderNo)
	if err != nil {
		return nil, wrapNotFound(err)
	}
	return order, nil
}

// ListOrders 分页查询订单列表
func ListOrders(page, pageSize int) ([]model.Order, int64, error) {
	return repository.ListOrders(page, pageSize)
}

func wrapNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New("订单不存在")
	}
	return err
}

// generateOrderNo 生成订单号：时间戳 + 微秒
func generateOrderNo() string {
	now := time.Now()
	return fmt.Sprintf("%s%06d", now.Format("20060102150405"), now.Nanosecond()/1000)
}
