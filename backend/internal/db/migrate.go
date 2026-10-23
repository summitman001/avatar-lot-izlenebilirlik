package db

import (
	"database/sql"
	"errors"
	"os"

	"github.com/golang-migrate/migrate/v4"
	mmysql "github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// Uretim/compose'ta migration'i `migrate/migrate` imaji calistirir. Bu yardimcilar testler icindir
// (conn multiStatements=true ile acilmis olmali).

func newMigrator(conn *sql.DB, dir string) (*migrate.Migrate, error) {
	drv, err := mmysql.WithInstance(conn, &mmysql.Config{})
	if err != nil {
		return nil, err
	}
	// iofs: file:// URL'i Windows surucu harfleriyle sorunlu oldugundan dizin dogrudan acilir.
	src, err := iofs.New(os.DirFS(dir), ".")
	if err != nil {
		return nil, err
	}
	return migrate.NewWithInstance("iofs", src, "mysql", drv)
}

func MigrateUp(conn *sql.DB, dir string) error {
	m, err := newMigrator(conn, dir)
	if err != nil {
		return err
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	return nil
}

func MigrateDown(conn *sql.DB, dir string) error {
	m, err := newMigrator(conn, dir)
	if err != nil {
		return err
	}
	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	return nil
}
