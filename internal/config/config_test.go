package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	content := `server:
  port: 9090
database:
  user: test
  password: secret
  host: db.example.com
  port: 3307
  name: demo
`
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("写入测试配置失败: %v", err)
	}

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
