// Package db baglanti acma ve (test/gelistirme icin) migration yardimcilari.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"
)

// Open DSN'i UTC + utf8mb4 + DATETIME->time.Time olacak sekilde zorlar ve baglantiyi bekler
// (MySQL konteyneri henuz hazir degilse ~30 sn yeniden dener).
func Open(ctx context.Context, dsn string, multiStatements bool) (*sql.DB, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("dsn: %w", err)
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.Collation = "utf8mb4_0900_ai_ci"
	cfg.MultiStatements = multiStatements
	cfg.ClientFoundRows = true // UPDATE'te RowsAffected = eslesen satir (degerler ayniysa da 404 olmasin)
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["time_zone"] = "'+00:00'" // oturum UTC: CURRENT_TIMESTAMP(3) UTC yazar

	conn, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(20)
	conn.SetMaxIdleConns(10)
	conn.SetConnMaxLifetime(5 * time.Minute)

	deadline := time.Now().Add(30 * time.Second)
	for {
		pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err = conn.PingContext(pctx)
		cancel()
		if err == nil {
			return conn, nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			conn.Close()
			return nil, fmt.Errorf("veritabani erisilemiyor: %w", err)
		}
		time.Sleep(time.Second)
	}
}
