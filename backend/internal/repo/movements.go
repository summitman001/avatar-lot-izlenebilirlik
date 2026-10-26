package repo

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
)

// stock_movement DEGISTIRILEMEZ bir defterdir (UPDATE/DELETE trigger'i hata verir).
// Duzeltme yalnizca ters kayitla (movement_type='reversal') yapilir.

const movementCols = `sm.id, sm.lot_id, l.lot_no, sm.warehouse_id, sm.movement_type, sm.qty, sm.unit_cost,
       sm.ref_type, sm.ref_id, sm.reverses_movement_id, sm.device_id, sm.client_created_at, sm.note, sm.created_at`

const movementFrom = ` FROM stock_movement sm JOIN lot l ON l.id = sm.lot_id`

func scanMovement(row interface{ Scan(...any) error }) (*domain.StockMovement, error) {
	var m domain.StockMovement
	err := row.Scan(&m.ID, &m.LotID, &m.LotNo, &m.WarehouseID, &m.MovementType, &m.Qty, &m.UnitCost,
		&m.RefType, &m.RefID, &m.ReversesMovementID, &m.DeviceID, &m.ClientCreatedAt, &m.Note, &m.CreatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return &m, nil
}

func getMovement(ctx context.Context, q DBTX, id string) (*domain.StockMovement, error) {
	return scanMovement(q.QueryRowContext(ctx, `SELECT `+movementCols+movementFrom+` WHERE sm.id = ?`, id))
}

func (s *Store) GetMovement(ctx context.Context, id string) (*domain.StockMovement, error) {
	return getMovement(ctx, s.db, id)
}

// insertMovement defter satirini ekler (id bos ise UUIDv7 uretilir) ve eklenen satiri doner.
// Cagiran fonksiyon kendi transaction'ini verir; hareket ile is kaydi hep birlikte yazilir.
func insertMovement(ctx context.Context, q DBTX, m domain.StockMovement) (*domain.StockMovement, error) {
	if m.ID == "" {
		m.ID = domain.NewID()
	}
	_, err := q.ExecContext(ctx,
		`INSERT INTO stock_movement (id, lot_id, warehouse_id, movement_type, qty, unit_cost, ref_type, ref_id,
		                             reverses_movement_id, device_id, client_created_at, note)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.LotID, m.WarehouseID, m.MovementType, m.Qty, m.UnitCost, m.RefType, m.RefID,
		m.ReversesMovementID, m.DeviceID, m.ClientCreatedAt, m.Note)
	if err != nil {
		return nil, mapErr(err)
	}
	return getMovement(ctx, q, m.ID)
}

type MovementFilter struct {
	LotID        string
	RefType      string
	RefID        string
	WarehouseID  string
	MovementType string
	Page         Page
}

func (s *Store) ListMovements(ctx context.Context, f MovementFilter) ([]domain.StockMovement, error) {
	var b query
	if f.LotID != "" {
		b.add(`sm.lot_id = ?`, f.LotID)
	}
	if f.RefType != "" {
		b.add(`sm.ref_type = ?`, f.RefType)
	}
	if f.RefID != "" {
		b.add(`sm.ref_id = ?`, f.RefID)
	}
	if f.WarehouseID != "" {
		b.add(`sm.warehouse_id = ?`, f.WarehouseID)
	}
	if f.MovementType != "" {
		b.add(`sm.movement_type = ?`, f.MovementType)
	}
	limit, offset := f.Page.normalized()
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+movementCols+movementFrom+b.sql()+` ORDER BY sm.created_at DESC, sm.id DESC LIMIT ? OFFSET ?`,
		append(b.args, limit, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.StockMovement{}
	for rows.Next() {
		m, err := scanMovement(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// reversibleRefTypes: tek basina geri alinabilen hareketlerin belge tipleri. Cok hareketli belgelerin
// (ör. production_order'in send_to_producer/transfer_in cifti) tek hareketi geri alinirsa belge ile
// defter ayrisir ve transferin yarisi geri alininca stok yoktan olusur. Yeni belge tipi buraya ancak
// tek hareketliyse eklenir.
var reversibleRefTypes = map[string]bool{"purchase_receipt": true}

// ReverseMovement bir hareketi ters kayitla geri alir: ayni lot/depo/birim maliyet, isareti ters qty.
//
// Kurallar:
//   - yalnizca reversibleRefTypes belgelerinin hareketleri geri alinabilir (422);
//   - bir ters kayit tekrar geri alinamaz;
//   - bir hareket en fazla bir kez geri alinabilir (UNIQUE reverses_movement_id; 409);
//   - geri alma stogu negatife dusuruyorsa reddedilir (409). Lot satiri kilitlenir (FOR UPDATE),
//     boylece ayni lota es zamanli islemler bakiye kontrolunu bozamaz.
func (s *Store) ReverseMovement(ctx context.Context, id string, req domain.ReverseRequest) (*domain.StockMovement, error) {
	var out *domain.StockMovement
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		// Idempotent tekrar: istemci ayni reversal id'siyle yeniden denerse mevcut kaydi don.
		if req.ID != nil {
			prev, err := getMovement(ctx, tx, *req.ID)
			if err == nil && prev.ReversesMovementID != nil && *prev.ReversesMovementID == id {
				out = prev
				return nil
			} else if err != nil && !errors.Is(err, domain.ErrNotFound) {
				return err
			}
		}
		orig, err := getMovement(ctx, tx, id)
		if err != nil {
			return err
		}
		if orig.MovementType == "reversal" {
			return domain.Invalid("ters kayit tekrar geri alinamaz")
		}
		if !reversibleRefTypes[orig.RefType] {
			return domain.Invalid("%s belgesine bagli hareket tek basina geri alinamaz; belgenin kendi akisiyla duzeltin", orig.RefType)
		}
		// Lot satirini kilitle, sonra bakiyeyi oku.
		var lotID string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM lot WHERE id = ? FOR UPDATE`, orig.LotID).Scan(&lotID); err != nil {
			return mapErr(err)
		}
		var already string
		err = tx.QueryRowContext(ctx, `SELECT id FROM stock_movement WHERE reverses_movement_id = ?`, id).Scan(&already)
		if err == nil {
			return domain.Conflict("hareket zaten geri alinmis (%s)", already)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		reversal := domain.StockMovement{
			LotID:              orig.LotID,
			WarehouseID:        orig.WarehouseID,
			MovementType:       "reversal",
			Qty:                domain.NewQty(orig.Qty.Neg()),
			UnitCost:           orig.UnitCost,
			RefType:            "stock_movement",
			RefID:              orig.ID,
			ReversesMovementID: &orig.ID,
			DeviceID:           req.DeviceID,
			Note:               req.Note,
		}
		if req.ID != nil {
			reversal.ID = *req.ID
		}
		if req.DeviceID != nil {
			now := time.Now().UTC()
			reversal.ClientCreatedAt = &now
		}

		var onHand domain.Qty
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(SUM(qty), 0) FROM stock_movement WHERE lot_id = ? AND warehouse_id = ?`,
			orig.LotID, orig.WarehouseID).Scan(&onHand); err != nil {
			return err
		}
		if onHand.Add(reversal.Qty.Decimal).IsNegative() {
			return domain.Conflict("geri alma stogu negatife dusurur (mevcut %s, degisim %s)",
				onHand.StringFixed(domain.QtyScale), reversal.Qty.StringFixed(domain.QtyScale))
		}
		out, err = insertMovement(ctx, tx, reversal)
		return err
	})
	return out, err
}
