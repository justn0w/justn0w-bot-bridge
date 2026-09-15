package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/viper"
	"github.com/subosito/gotenv"
)

const (
	// envFile 指定 dotenv 文件路径，可用 ENV_FILE 覆盖（便于测试与多环境部署）
	envFile = "ENV_FILE"

	envFeishuAppID     = "FEISHU_APP_ID"
	envFeishuAppSecret = "FEISHU_APP_SECRET"
)

// envBindings 声明「配置键 <- 环境变量」的绑定关系。
// 敏感配置只从环境变量注入，不写入 YAML，避免随代码库泄露。
var envBindings = []struct {
	key string
	env string
}{
	{"feishu.app_id", envFeishuAppID},
	{"feishu.app_secret", envFeishuAppSecret},
}

// Config 全局配置
type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	Feishu   FeishuConfig   `mapstructure:"feishu"`
}

// ServerConfig 服务配置
type ServerConfig struct {
	Port int `mapstructure:"port"`
}

// DatabaseConfig 数据库配置
type DatabaseConfig struct {
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Name     string `mapstructure:"name"`
}

// FeishuConfig 飞书应用配置（敏感信息，仅通过 .env / 环境变量注入）
type FeishuConfig struct {
	AppID     string `mapstructure:"app_id"`
	AppSecret string `mapstructure:"app_secret"`
}

// Load 加载配置：先注入 .env，再读取 YAML，最后校验必填项
func Load() (*Config, error) {
	if err := loadDotEnv(); err != nil {
		return nil, err
	}

	cfg, err := load("config", []string{"./configs", "."})
	if err != nil {
		return nil, err
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// load 从指定配置名与搜索路径加载 YAML 配置（路径可注入，便于测试）
func load(name string, paths []string) (*Config, error) {
	v := viper.New()
	v.SetConfigName(name)
	v.SetConfigType("yaml")
	for _, p := range paths {
		v.AddConfigPath(p)
	}

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	// 环境变量优先级高于 YAML，未设置时回退到 YAML 中的值
	for _, b := range envBindings {
		if err := v.BindEnv(b.key, b.env); err != nil {
			return nil, fmt.Errorf("绑定环境变量 %s 失败: %w", b.env, err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}
	return &cfg, nil
}

// loadDotEnv 将 .env 中的变量注入进程环境。
// 文件不存在时静默跳过——生产环境直接注入环境变量即可。
// 已存在的环境变量优先，不会被 .env 覆盖。
func loadDotEnv() error {
	path := os.Getenv(envFile)
	if path == "" {
		path = ".env"
	}

	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err := gotenv.Load(path); err != nil {
		return fmt.Errorf("加载 %s 失败: %w", path, err)
	}
	return nil
}

// Validate 校验必填配置，缺失时快速失败并给出可操作的提示
func (c *Config) Validate() error {
	if c.Feishu.AppID == "" {
		return fmt.Errorf("飞书 AppID 未配置，请设置环境变量 %s（可参考 .env.example）", envFeishuAppID)
	}
	if c.Feishu.AppSecret == "" {
		return fmt.Errorf("飞书 AppSecret 未配置，请设置环境变量 %s（可参考 .env.example）", envFeishuAppSecret)
	}
	return nil
}

// DSN 返回 MySQL 连接串
func (c *Config) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		c.Database.User, c.Database.Password, c.Database.Host, c.Database.Port, c.Database.Name)
}
