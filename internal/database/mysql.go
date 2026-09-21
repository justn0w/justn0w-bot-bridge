package database

import (
	"log"

	"justn0w-bot-bridge/internal/config"
	"justn0w-bot-bridge/internal/model"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// db 全局数据库客户端：由 Init 初始化，测试通过 SetDB 注入
var db *gorm.DB

// Init 初始化 MySQL 连接并自动迁移表结构
func Init(cfg *config.Config) {
	d, err := gorm.Open(mysql.Open(cfg.DSN()), &gorm.Config{})
	if err != nil {
		log.Fatalf("连接数据库失败: %v", err)
	}

	if err := d.AutoMigrate(&model.Order{}); err != nil {
		log.Fatalf("自动迁移失败: %v", err)
	}

	db = d
	log.Println("数据库连接成功，表结构迁移完成")
}

// GetDB 返回当前数据库客户端
func GetDB() *gorm.DB {
	return db
}

// SetDB 注入数据库客户端（仅供测试使用）
func SetDB(d *gorm.DB) {
	db = d
}
