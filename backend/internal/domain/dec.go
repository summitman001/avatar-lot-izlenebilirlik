// Package domain: ORM'siz, I/O'suz saf is kurallari (decimal tipleri, yuvarlama, maliyet hesabi).
// Float hicbir yerde kullanilmaz; JSON'da decimal degerler STRING tasinir.
package domain

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

// Qty: miktar, DECIMAL(14,3). JSON'da her zaman 3 ondalikli string ("25.000").
// sql.Scanner / driver.Valuer, gomulu decimal.Decimal'den gelir.
type Qty struct{ decimal.Decimal }

// Amount: tutar ve birim maliyet, DECIMAL(14,4). JSON'da her zaman 4 ondalikli string ("18.1588").
type Amount struct{ decimal.Decimal }

const (
	QtyScale    = 3
	AmountScale = 4
)

func NewQty(d decimal.Decimal) Qty       { return Qty{d} }
func NewAmount(d decimal.Decimal) Amount { return Amount{d} }

func (q Qty) MarshalJSON() ([]byte, error)    { return json.Marshal(q.StringFixed(QtyScale)) }
func (a Amount) MarshalJSON() ([]byte, error) { return json.Marshal(a.StringFixed(AmountScale)) }

func (q *Qty) UnmarshalJSON(b []byte) error {
	d, err := decodeDecimal(b, QtyScale)
	q.Decimal = d
	return err
}

func (a *Amount) UnmarshalJSON(b []byte) error {
	d, err := decodeDecimal(b, AmountScale)
	a.Decimal = d
	return err
}

// decodeDecimal yalnizca JSON STRING kabul eder: JSON number (18.1588) float'a donusme riski
// tasidigi icin reddedilir. Sutun olceginden fazla ondalik de reddedilir (sessiz yuvarlama yok).
func decodeDecimal(b []byte, scale int32) (decimal.Decimal, error) {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || b[0] != '"' {
		return decimal.Zero, fmt.Errorf("decimal degerler JSON string olmali (ornek \"18.1588\")")
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return decimal.Zero, err
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero, fmt.Errorf("gecersiz decimal %q", s)
	}
	if !d.Round(scale).Equal(d) {
		return decimal.Zero, fmt.Errorf("%q en fazla %d ondalik olabilir", s, scale)
	}
	return d, nil
}

// Date: takvim gunu (DATE kolonu). JSON'da "2006-01-02".
type Date struct{ time.Time }

const dateLayout = "2006-01-02"

func (d Date) MarshalJSON() ([]byte, error) { return json.Marshal(d.Format(dateLayout)) }

func (d *Date) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("tarih \"YYYY-MM-DD\" string olmali")
	}
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return fmt.Errorf("gecersiz tarih %q (beklenen YYYY-MM-DD)", s)
	}
	d.Time = t
	return nil
}

// Scan DATE kolonunu okur (parseTime=true ile time.Time, aksi halde []byte/string gelir).
func (d *Date) Scan(v any) error {
	switch x := v.(type) {
	case time.Time:
		d.Time = x
	case []byte:
		return d.parse(string(x))
	case string:
		return d.parse(x)
	default:
		return fmt.Errorf("Date.Scan: desteklenmeyen tip %T", v)
	}
	return nil
}

func (d *Date) parse(s string) error {
	t, err := time.Parse(dateLayout, s)
	d.Time = t
	return err
}

// Value surucuye "YYYY-MM-DD" string olarak gider (saat dilimi kaymasi olmaz).
func (d Date) Value() (driver.Value, error) { return d.Format(dateLayout), nil }

// NewDate saat bilgisini atarak UTC gun olusturur.
func NewDate(y int, m time.Month, day int) Date {
	return Date{time.Date(y, m, day, 0, 0, 0, 0, time.UTC)}
}

// ExpiryFrom: SKT acikca verilmisse o; verilmemisse lot tarihi + urunun raf omru; raf omru de
// tanimsizsa nil (lot SKT'siz kalir).
func ExpiryFrom(explicit *Date, lotDate Date, shelfLifeDays *int) *Date {
	if explicit != nil || shelfLifeDays == nil {
		return explicit
	}
	return &Date{lotDate.AddDate(0, 0, *shelfLifeDays)}
}

// Today sunucunun bugunku UTC tarihini doner (ornek: receive aninda uretilen cikti lotunun tarihi).
func Today() Date {
	t := time.Now().UTC()
	return NewDate(t.Year(), t.Month(), t.Day())
}
