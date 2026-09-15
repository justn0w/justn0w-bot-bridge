package config

import (
	"os"
	"path/filepath"
	"testing"
)

const baseYAML = `server:
  port: 9090
database:
  user: test
  password: secret
  host: db.example.com
  port: 3307
  name: demo
`

// writeFile 在 dir 下写入文件，失败即终止测试
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写入 %s 失败: %v", name, err)
	}
	return path
}

// unsetenv 在测试期间移除环境变量，结束后恢复。
// t.Setenv 只支持赋值，先用它记录原始状态，再真正取消设置。
func unsetenv(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("取消设置 %s 失败: %v", key, err)
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.yaml", baseYAML)

	cfg, err := load("config", []string{dir})
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}

	if cfg.Server.Port != 9090 {
		t.Errorf("Server.Port = %d, want 9090", cfg.Server.Port)
	}

	db := cfg.Database
	if db.User != "test" || db.Password != "secret" || db.Host != "db.example.com" ||
		db.Port != 3307 || db.Name != "demo" {
		t.Errorf("Database = %+v", db)
	}

	wantDSN := "test:secret@tcp(db.example.com:3307)/demo?charset=utf8mb4&parseTime=True&loc=Local"
	if got := cfg.DSN(); got != wantDSN {
		t.Errorf("DSN() = %q, want %q", got, wantDSN)
	}
}

func TestLoadFeishuFromEnv(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.yaml", baseYAML)

	t.Setenv(envFeishuAppID, "cli_from_env")
	t.Setenv(envFeishuAppSecret, "secret_from_env")

	cfg, err := load("config", []string{dir})
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}

	if cfg.Feishu.AppID != "cli_from_env" {
		t.Errorf("Feishu.AppID = %q, want %q", cfg.Feishu.AppID, "cli_from_env")
	}
	if cfg.Feishu.AppSecret != "secret_from_env" {
		t.Errorf("Feishu.AppSecret = %q, want %q", cfg.Feishu.AppSecret, "secret_from_env")
	}
}

func TestLoadFeishuEmptyWhenEnvUnset(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.yaml", baseYAML)

	unsetenv(t, envFeishuAppID)
	unsetenv(t, envFeishuAppSecret)

	cfg, err := load("config", []string{dir})
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}

	// 凭证不应来自 YAML，缺失时必须为空，交由 Validate 拦截
	if cfg.Feishu.AppID != "" || cfg.Feishu.AppSecret != "" {
		t.Errorf("Feishu = %+v, want 空值", cfg.Feishu)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		feishu  FeishuConfig
		wantErr bool
	}{
		{
			name:    "凭证齐全时通过",
			feishu:  FeishuConfig{AppID: "cli_x", AppSecret: "s3cret"},
			wantErr: false,
		},
		{
			name:    "缺少 AppID 时报错",
			feishu:  FeishuConfig{AppSecret: "s3cret"},
			wantErr: true,
		},
		{
			name:    "缺少 AppSecret 时报错",
			feishu:  FeishuConfig{AppID: "cli_x"},
			wantErr: true,
		},
		{
			name:    "两者都缺失时报错",
			feishu:  FeishuConfig{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Feishu: tt.feishu}
			if err := cfg.Validate(); (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoadDotEnvLoadsFile(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, ".env", "FEISHU_APP_ID=cli_from_dotenv\nFEISHU_APP_SECRET=secret_from_dotenv\n")

	unsetenv(t, envFeishuAppID)
	unsetenv(t, envFeishuAppSecret)
	t.Setenv(envFile, path)

	if err := loadDotEnv(); err != nil {
		t.Fatalf("loadDotEnv() = %v", err)
	}

	if got := os.Getenv(envFeishuAppID); got != "cli_from_dotenv" {
		t.Errorf("%s = %q, want %q", envFeishuAppID, got, "cli_from_dotenv")
	}
	if got := os.Getenv(envFeishuAppSecret); got != "secret_from_dotenv" {
		t.Errorf("%s = %q, want %q", envFeishuAppSecret, got, "secret_from_dotenv")
	}
}

func TestLoadDotEnvMissingFileIsNotError(t *testing.T) {
	t.Setenv(envFile, filepath.Join(t.TempDir(), "does-not-exist.env"))

	if err := loadDotEnv(); err != nil {
		t.Errorf("loadDotEnv() = %v, want nil（文件缺失应静默跳过）", err)
	}
}

func TestLoadDotEnvDoesNotOverrideExistingEnv(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, ".env", "FEISHU_APP_ID=cli_from_dotenv\n")

	// 已有的环境变量优先，.env 不得覆盖（生产环境注入优先于本地文件）
	t.Setenv(envFeishuAppID, "cli_from_real_env")
	t.Setenv(envFile, path)

	if err := loadDotEnv(); err != nil {
		t.Fatalf("loadDotEnv() = %v", err)
	}

	if got := os.Getenv(envFeishuAppID); got != "cli_from_real_env" {
		t.Errorf("%s = %q, want %q（真实环境变量应优先）", envFeishuAppID, got, "cli_from_real_env")
	}
}

func TestLoadDotEnvInvalidFileReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "broken.env", "this line has no separator\n")

	t.Setenv(envFile, path)

	if err := loadDotEnv(); err == nil {
		t.Error("loadDotEnv() = nil, want error（文件格式非法时应报错而非静默忽略）")
	}
}

// TestLoadEndToEnd 覆盖 main.go 实际调用路径：cwd 下的 .env + configs/config.yaml
func TestLoadEndToEnd(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "configs"), 0o755); err != nil {
		t.Fatalf("创建 configs 目录失败: %v", err)
	}
	writeFile(t, filepath.Join(dir, "configs"), "config.yaml", baseYAML)
	writeFile(t, dir, ".env", "FEISHU_APP_ID=cli_e2e\nFEISHU_APP_SECRET=secret_e2e\n")

	unsetenv(t, envFeishuAppID)
	unsetenv(t, envFeishuAppSecret)
	t.Chdir(dir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}

	if cfg.Feishu.AppID != "cli_e2e" || cfg.Feishu.AppSecret != "secret_e2e" {
		t.Errorf("Feishu = %+v, want 来自 .env 的凭证", cfg.Feishu)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("Server.Port = %d, want 9090", cfg.Server.Port)
	}
}

// TestLoadFailsWhenCredentialsMissing 确认缺少凭证时快速失败，而不是启动后才报错
func TestLoadFailsWhenCredentialsMissing(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "configs"), 0o755); err != nil {
		t.Fatalf("创建 configs 目录失败: %v", err)
	}
	writeFile(t, filepath.Join(dir, "configs"), "config.yaml", baseYAML)

	unsetenv(t, envFeishuAppID)
	unsetenv(t, envFeishuAppSecret)
	t.Chdir(dir)

	if _, err := Load(); err == nil {
		t.Error("Load() = nil, want error（缺少飞书凭证时应快速失败）")
	}
}
