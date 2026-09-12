-- 创建数据库
CREATE DATABASE IF NOT EXISTS justn0w_bot_bridge
  DEFAULT CHARACTER SET utf8mb4
  COLLATE utf8mb4_unicode_ci;

USE justn0w_bot_bridge;

-- ---------------------------------------------------------------------------
-- orders 订单表
-- 表结构与 GORM 模型 internal/model/order.go 保持一致，
-- 便于在未启动服务（AutoMigrate）前直接执行本脚本完成初始化。
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS orders (
  id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  order_no     VARCHAR(64)     NOT NULL COMMENT '订单号',
  user_id      BIGINT UNSIGNED NOT NULL COMMENT '用户ID',
  product_name VARCHAR(255)    NOT NULL COMMENT '商品名称',
  amount       DECIMAL(10, 2)  NOT NULL COMMENT '订单金额',
  status       TINYINT         NOT NULL DEFAULT 0 COMMENT '订单状态: 0待支付 1已支付 2已取消',
  created_at   DATETIME(3)     NULL,
  updated_at   DATETIME(3)     NULL,
  PRIMARY KEY (id),
  UNIQUE KEY idx_orders_order_no (order_no),
  KEY idx_orders_user_id (user_id)
) ENGINE = InnoDB
  DEFAULT CHARACTER SET utf8mb4
  COLLATE utf8mb4_unicode_ci
  COMMENT = '订单表';

-- ---------------------------------------------------------------------------
-- 初始化示例数据（status: 0待支付 / 1已支付 / 2已取消）
-- ---------------------------------------------------------------------------
INSERT INTO orders (order_no, user_id, product_name, amount, status, created_at, updated_at) VALUES
  ('ORD202409010001', 1001, 'iPhone 15',           5999.00, 1, NOW() - INTERVAL 10 DAY, NOW() - INTERVAL 10 DAY),
  ('ORD202409020002', 1001, 'AirPods Pro',         1899.00, 1, NOW() - INTERVAL 9 DAY,  NOW() - INTERVAL 9 DAY),
  ('ORD202409030003', 1002, 'MacBook Air M3',      8999.00, 0, NOW() - INTERVAL 8 DAY,  NOW() - INTERVAL 8 DAY),
  ('ORD202409040004', 1002, 'iPad mini',           3999.00, 2, NOW() - INTERVAL 7 DAY,  NOW() - INTERVAL 7 DAY),
  ('ORD202409050005', 1003, 'Apple Watch S9',      2999.00, 1, NOW() - INTERVAL 6 DAY,  NOW() - INTERVAL 6 DAY),
  ('ORD202409060006', 1003, 'Magic Keyboard',       899.00, 0, NOW() - INTERVAL 5 DAY,  NOW() - INTERVAL 5 DAY),
  ('ORD202409070007', 1001, 'iPhone 15 Pro Max',   9999.00, 1, NOW() - INTERVAL 4 DAY,  NOW() - INTERVAL 4 DAY),
  ('ORD202409080008', 1004, 'Sony WH-1000XM5',     2499.00, 2, NOW() - INTERVAL 3 DAY,  NOW() - INTERVAL 3 DAY),
  ('ORD202409090009', 1004, 'Logitech MX Master 3S', 699.00, 1, NOW() - INTERVAL 2 DAY, NOW() - INTERVAL 2 DAY),
  ('ORD202409100010', 1002, 'Dyson V12',           3599.00, 0, NOW() - INTERVAL 1 DAY,  NOW() - INTERVAL 1 DAY),
  ('ORD202409110011', 1003, 'Kindle Paperwhite',   1099.00, 1, NOW() - INTERVAL 12 HOUR, NOW() - INTERVAL 12 HOUR),
  ('ORD202409120012', 1001, 'Anker 充电器',         159.00, 0, NOW() - INTERVAL 1 HOUR,  NOW() - INTERVAL 1 HOUR);
