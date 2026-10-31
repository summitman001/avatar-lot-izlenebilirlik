// Package repo: database/sql ile SQL katmani (ORM yok). Her yazma fonksiyonu kendi transaction'ini acar;
// sync'e acik tablolara (item, customer, warehouse, lot) yazan her fonksiyon change_log kaydini
// AYNI transaction'da atar.
package repo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/go-sql-driver/mysql"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/mlclient"
)

// DBTX hem *sql.DB hem *sql.Tx tarafindan saglanir; okuma fonksiyonlari ikisiyle de calisir.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type Store struct {
	db         *sql.DB
	ml         *mlclient.Client // nil ise ML entegrasyonu atlanir (bkz. production.go, predictOnCreate)
	uploadsDir string           // fatura/irsaliye OCR yuklemeleri (bkz. invoice_import.go), varsayilan "./uploads"
}

func New(db *sql.DB) *Store { return &Store{db: db, uploadsDir: "./uploads"} }

func (s *Store) DB() *sql.DB { return s.db }

// SetMLClient ML tahmin istemcisini baglar (zincirlenebilir: repo.New(db).SetMLClient(c)). nil
// gecilebilir (ML'siz calisma, ornegin testlerde).
func (s *Store) SetMLClient(c *mlclient.Client) *Store {
	s.ml = c
	return s
}

// SetUploadsDir yuklenen fatura/irsaliye gorsellerinin saklanacagi dizini belirler
// (zincirlenebilir). Varsayilan "./uploads".
func (s *Store) SetUploadsDir(dir string) *Store {
	if dir != "" {
		s.uploadsDir = dir
	}
	return s
}

// InTx fn'i tek transaction'da calistirir; hata donerse geri alir.
func (s *Store) InTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// MySQL hata numaralari
const (
	errDupEntry        = 1062
	errNoReferencedRow = 1452
	errRowIsReferenced = 1451
	errCheckViolated   = 3819
	errSignal          = 1644 // SIGNAL SQLSTATE '45000' (stock_movement trigger'lari)
)

// mapErr surucu hatalarini domain hatalarina cevirir (HTTP katmani status'a cevirir).
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	var me *mysql.MySQLError
	if errors.As(err, &me) {
		switch me.Number {
		case errDupEntry:
			return domain.Conflict("bu numara veya kod zaten kullaniliyor: %s", dupValue(me.Message))
		case errNoReferencedRow:
			return domain.Invalid("referans verilen kayit bulunamadi")
		case errRowIsReferenced:
			return domain.Conflict("kayit baska kayitlarca kullaniliyor")
		case errCheckViolated:
			return domain.Invalid("veritabani kisiti ihlal edildi: %s", me.Message)
		case errSignal:
			return domain.Conflict("%s", me.Message)
		}
	}
	return err
}

// dupValue "Duplicate entry 'X' for key 'lot.uq_lot_no'" mesajindan yalnizca X'i cikarir: tablo ve
// indeks adlari kullaniciya gosterilmez.
func dupValue(msg string) string {
	start := strings.Index(msg, "'")
	end := strings.LastIndex(msg, "' for key")
	if start < 0 || end <= start {
		return msg
	}
	return msg[start+1 : end]
}

// resolveID istemci ID'sini dogrular ya da yeni UUIDv7 uretir.
func resolveID(id string) (string, error) {
	if id == "" {
		return domain.NewID(), nil
	}
	return domain.ParseID(id)
}

func resolveIDPtr(id *string) (string, error) {
	if id == nil {
		return domain.NewID(), nil
	}
	return domain.ParseID(*id)
}

// appendChange sync cekme akisina (change_log) kayit ekler. Cagiran, varligi yazdigi transaction'i verir.
func appendChange(ctx context.Context, q DBTX, entity, entityID, action string, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	// JSON kolonuna string olarak gonderilir ([]byte binary charset hatasi verir).
	_, err = q.ExecContext(ctx,
		`INSERT INTO change_log (entity, entity_id, action, payload_json) VALUES (?, ?, ?, ?)`,
		entity, entityID, action, string(b))
	return err
}

// Page liste sorgularinin sayfalamasi.
type Page struct{ Limit, Offset int }

const (
	DefaultLimit = 200
	MaxLimit     = 1000
)

func (p Page) normalized() (int, int) {
	l, o := p.Limit, p.Offset
	if l <= 0 {
		l = DefaultLimit
	}
	if l > MaxLimit {
		l = MaxLimit
	}
	if o < 0 {
		o = 0
	}
	return l, o
}

// query kucuk bir WHERE olusturucudur.
type query struct {
	where []string
	args  []any
}

func (b *query) add(cond string, args ...any) {
	b.where = append(b.where, cond)
	b.args = append(b.args, args...)
}

func (b *query) sql() string {
	if len(b.where) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(b.where, " AND ")
}

// like LIKE kalibi icin %, _ ve \ karakterlerini kacirir.
func like(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(s) + "%"
}
