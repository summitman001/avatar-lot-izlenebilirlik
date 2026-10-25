// Package config ortam degiskenlerinden ayarlari okur (12-factor).
package config

import (
	"os"
	"strings"

	"github.com/go-sql-driver/mysql"
)

type Config struct {
	HTTPAddr    string
	DSN         string   // go-sql-driver DSN
	CORSOrigins []string // "*" = hepsi
	GoldenDir   string   // seed: docs/golden dizini
	MLURL       string   // ML tahmin servisi (bkz. internal/mlclient); erisilemezse tahmin atlanir
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Load DATABASE_URL (Go DSN) varsa onu, yoksa DB_HOST/DB_PORT/DB_NAME/DB_USER/DB_PASSWORD'u kullanir.
func Load() Config {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		c := mysql.NewConfig()
		c.User = env("DB_USER", "app")
		c.Passwd = env("DB_PASSWORD", "app")
		c.Net = "tcp"
		c.Addr = env("DB_HOST", "127.0.0.1") + ":" + env("DB_PORT", "3306")
		c.DBName = env("DB_NAME", "avatar")
		dsn = c.FormatDSN()
	}
	var origins []string
	for _, o := range strings.Split(env("CORS_ORIGINS", "*"), ",") {
		if o = strings.TrimSpace(o); o != "" {
			origins = append(origins, o)
		}
	}
	return Config{
		HTTPAddr:    env("HTTP_ADDR", ":8080"),
		DSN:         dsn,
		CORSOrigins: origins,
		GoldenDir:   env("GOLDEN_DIR", "../docs/golden"),
		MLURL:       env("ML_URL", "http://localhost:8000"),
	}
}
