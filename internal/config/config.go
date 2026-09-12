package config

import (
	"fmt"

	"github.com/spf13/viper"
)

// Config 全局配置
type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
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

// Load 从默认路径（./configs 与当前目录）加载配置
func Load() (*Config, error) {
	return load("config", []string{"./configs", "."})
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

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}
	return &cfg, nil
}

// DSN 返回 MySQL 连接串
func (c *Config) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		c.Database.User, c.Database.Password, c.Database.Host, c.Database.Port, c.Database.Name)
}
