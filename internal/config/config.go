package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/viper"
	"github.com/subosito/gotenv"
)

const (
	// envFile 指定 dotenv 文件路径，可用 ENV_FILE 覆盖（便于测试与多环境部署）
	envFile = "ENV_FILE"

	envFeishuAppID     = "FEISHU_APP_ID"
	envFeishuAppSecret = "FEISHU_APP_SECRET"
)

// 答疑 agent 的默认值，可在 config.yaml 的 claude 段覆盖
const (
	// defaultClaudeCLIPath 按名称交给 PATH 查找；部署路径不同时再改配置
	defaultClaudeCLIPath = "claude"
	// defaultClaudeTimeoutSec 给得宽松：每次提问都要冷启动一个
	// Node 子进程并加载代码库上下文，慢是常态
	defaultClaudeTimeoutSec = 120
)

// envBindings 声明「配置键 <- 环境变量」的绑定关系。
// 约定是敏感配置只从环境变量注入、不写入 YAML，避免随代码库泄露。
//
// 注意：BindEnv 只保证环境变量「优先」，并不「独占」——环境变量缺失时
// 仍会回落到 YAML 中的同名键。若要让密钥真正做到 env-only，需要在此显式
// 清空 YAML 来源的取值，目前尚未强制。
var envBindings = []struct {
	key string
	// envs 按优先级排列，靠前的先取值
	envs []string
}{
	{"feishu.app_id", []string{envFeishuAppID}},
	{"feishu.app_secret", []string{envFeishuAppSecret}},
}

// Config 全局配置
type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	Feishu   FeishuConfig   `mapstructure:"feishu"`
	Claude   ClaudeConfig   `mapstructure:"claude"`
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

// ClaudeConfig 是 Claude Code CLI 答疑 agent 的配置。
// 不需要密钥：CLI 复用本机的 Claude Code 登录态。
type ClaudeConfig struct {
	// CLIPath 是 claude 可执行文件路径，留空按 "claude" 走 PATH 查找
	CLIPath string `mapstructure:"cli_path"`
	// TimeoutSec 是单次调用的超时秒数
	TimeoutSec int `mapstructure:"timeout_sec"`
}

// Timeout 返回单次 CLI 调用的超时时间
func (c ClaudeConfig) Timeout() time.Duration {
	return time.Duration(c.TimeoutSec) * time.Second
}

// LoadOption 调整 Load 的行为
type LoadOption func(*loadOptions)

type loadOptions struct {
	// skipCredentials 跳过飞书凭证校验
	skipCredentials bool
}

// WithoutCredentialCheck 跳过飞书凭证校验。
//
// 供不依赖答疑链路的入口使用（如订单 MCP 服务）：它只需要数据库配置，
// 强制要求飞书凭证会让它在没配这些凭证的机器上直接起不来。
func WithoutCredentialCheck() LoadOption {
	return func(o *loadOptions) { o.skipCredentials = true }
}

// Load 加载配置：先注入 .env，再读取 YAML，最后校验必填项
func Load(opts ...LoadOption) (*Config, error) {
	var o loadOptions
	for _, opt := range opts {
		opt(&o)
	}

	if err := loadDotEnv(); err != nil {
		return nil, err
	}

	cfg, err := load("config", []string{"./configs", "."})
	if err != nil {
		return nil, err
	}

	if o.skipCredentials {
		return cfg, nil
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
		// BindEnv 的唯一形参就是变参，切片展开必须整体提供，
		// 不能写成 v.BindEnv(b.key, b.envs...)，故先拼成一个切片。
		args := append([]string{b.key}, b.envs...)
		if err := v.BindEnv(args...); err != nil {
			return nil, fmt.Errorf("绑定环境变量 %v 失败: %w", b.envs, err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}

	// 可选配置留空时回落到默认值，避免调用方各自处理零值
	cfg.applyDefaults()
	return &cfg, nil
}

// applyDefaults 为未显式配置的可选字段填充默认值
func (c *Config) applyDefaults() {
	if c.Claude.CLIPath == "" {
		c.Claude.CLIPath = defaultClaudeCLIPath
	}
	if c.Claude.TimeoutSec <= 0 {
		c.Claude.TimeoutSec = defaultClaudeTimeoutSec
	}
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
