package domain

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// Hata siniflari. HTTP katmani bunlari kod/status'a cevirir (internal/http/errors.go).
var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

// ValidationError: istemcinin duzeltebilecegi gecersiz girdi (HTTP 422).
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func Invalid(format string, args ...any) error {
	return &ValidationError{Msg: fmt.Sprintf(format, args...)}
}

// ConflictError: durumla celisen istek (tekrarlanan numara, yetersiz stok, ...) (HTTP 409).
type ConflictError struct{ Msg string }

func (e *ConflictError) Error() string { return e.Msg }
func (e *ConflictError) Unwrap() error { return ErrConflict }

func Conflict(format string, args ...any) error {
	return &ConflictError{Msg: fmt.Sprintf(format, args...)}
}

// NewID yeni UUIDv7 uretir (zaman sirali; istemci tarafinda da uretilebilir).
func NewID() string {
	id, err := uuid.NewV7()
	if err != nil {
		panic(err) // yalnizca rastgelelik kaynagi bozuksa
	}
	return id.String()
}

// ParseID istemciden gelen ID'nin UUIDv7 oldugunu dogrular.
func ParseID(s string) (string, error) {
	id, err := uuid.Parse(s)
	if err != nil || id.Version() != 7 || len(s) != 36 {
		return "", Invalid("%q gecerli bir UUIDv7 degil", s)
	}
	return id.String(), nil
}
