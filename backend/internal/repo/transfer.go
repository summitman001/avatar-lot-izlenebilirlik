package repo

import (
	"context"
	"database/sql"
	"errors"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
)

// Package repo (transfer.go): depolar arasi transfer (migration 000011). Her satir AYNI transaction'da
// kaynak depoda transfer_out (-) ve hedef depoda transfer_in (+) hareketi yazar; lot satiri FOR UPDATE
// kilitlenir, kaynak depo bakiyesi yetmezse 409 (negatif stok yok). Lot maliyeti degismez. Bloke /
// geri cagrilmis lot yalnizca karantina deposuna tasinabilir. Bakiye degistigi icin lot change_log'a
// yazilir: arac deposundaki cihaz yeni stogu pull'da gorur (bkz. docs/sync.md).

const stockTransferCols = `t.id, t.transfer_no, t.from_warehouse_id, fw.code, t.to_warehouse_id, tw.code, t.note, t.created_at`

const stockTransferFrom = ` FROM stock_transfer t
JOIN warehouse fw ON fw.id = t.from_warehouse_id
JOIN warehouse tw ON tw.id = t.to_warehouse_id`

func scanStockTransfer(row interface{ Scan(...any) error }) (*domain.StockTransfer, error) {
	var t domain.StockTransfer
	if err := row.Scan(&t.ID, &t.TransferNo, &t.FromWarehouseID, &t.FromWarehouseCode, &t.ToWarehouseID,
		&t.ToWarehouseCode, &t.Note, &t.CreatedAt); err != nil {
		return nil, mapErr(err)
	}
	return &t, nil
}

func getStockTransfer(ctx context.Context, q DBTX, id string) (*domain.StockTransfer, error) {
	t, err := scanStockTransfer(q.QueryRowContext(ctx, `SELECT `+stockTransferCols+stockTransferFrom+` WHERE t.id = ?`, id))
	if err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx,
		`SELECT sm.lot_id, l.lot_no, l.item_id, sm.qty
		 FROM stock_movement sm JOIN lot l ON l.id = sm.lot_id
		 WHERE sm.ref_type = 'stock_transfer' AND sm.ref_id = ? AND sm.movement_type = 'transfer_in'
		 ORDER BY sm.created_at, l.lot_no`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	t.Lines = []domain.StockTransferLine{}
	for rows.Next() {
		var l domain.StockTransferLine
		if err := rows.Scan(&l.LotID, &l.LotNo, &l.ItemID, &l.Qty); err != nil {
			return nil, err
		}
		t.Lines = append(t.Lines, l)
	}
	return t, rows.Err()
}

func (s *Store) GetStockTransfer(ctx context.Context, id string) (*domain.StockTransfer, error) {
	return getStockTransfer(ctx, s.db, id)
}

type StockTransferFilter struct {
	WarehouseID string // kaynak VEYA hedef
	LotID       string
	Page        Page
}

func (s *Store) ListStockTransfers(ctx context.Context, f StockTransferFilter) ([]domain.StockTransfer, error) {
	var b query
	if f.WarehouseID != "" {
		b.add(`(t.from_warehouse_id = ? OR t.to_warehouse_id = ?)`, f.WarehouseID, f.WarehouseID)
	}
	if f.LotID != "" {
		b.add(`EXISTS (SELECT 1 FROM stock_movement x WHERE x.ref_type = 'stock_transfer' AND x.ref_id = t.id AND x.lot_id = ?)`, f.LotID)
	}
	limit, offset := f.Page.normalized()
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+stockTransferCols+stockTransferFrom+b.sql()+` ORDER BY t.created_at DESC, t.transfer_no DESC LIMIT ? OFFSET ?`,
		append(b.args, limit, offset)...)
	if err != nil {
		return nil, err
	}
	var out []domain.StockTransfer
	for rows.Next() {
		t, err := scanStockTransfer(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, *t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out { // satirlar ayri sorgu: liste rows'u kapandiktan SONRA
		full, err := getStockTransfer(ctx, s.db, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Lines = full.Lines
	}
	if out == nil {
		out = []domain.StockTransfer{}
	}
	return out, nil
}

// CreateStockTransfer transferi tek transaction'da yazar. Ayni istemci id'siyle tekrar gelen istek
// mevcut transferi doner (idempotency).
func (s *Store) CreateStockTransfer(ctx context.Context, req domain.CreateStockTransferRequest) (*domain.StockTransfer, error) {
	if req.ID != nil {
		existing, err := s.GetStockTransfer(ctx, *req.ID)
		if err == nil {
			return existing, nil
		} else if !errors.Is(err, domain.ErrNotFound) {
			return nil, err
		}
	}
	id, err := resolveIDPtr(req.ID)
	if err != nil {
		return nil, err
	}
	err = s.InTx(ctx, func(tx *sql.Tx) error {
		var toType string
		for field, whID := range map[string]string{"from_warehouse_id": req.FromWarehouseID, "to_warehouse_id": req.ToWarehouseID} {
			var whType string
			err := tx.QueryRowContext(ctx, `SELECT warehouse_type FROM warehouse WHERE id = ?`, whID).Scan(&whType)
			if errors.Is(err, sql.ErrNoRows) {
				return domain.Invalid("%s bulunamadi", field)
			} else if err != nil {
				return err
			}
			if field == "to_warehouse_id" {
				toType = whType
			}
		}
		transferNo, err := nextSequenceNo(ctx, tx, "TRF")
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO stock_transfer (id, transfer_no, from_warehouse_id, to_warehouse_id, note) VALUES (?, ?, ?, ?, ?)`,
			id, transferNo, req.FromWarehouseID, req.ToWarehouseID, req.Note); err != nil {
			return mapErr(err)
		}
		for i, line := range req.Lines {
			lot, err := lockedLotForSale(ctx, tx, line.LotID)
			if errors.Is(err, domain.ErrNotFound) {
				return domain.Invalid("lines[%d].lot_id bulunamadi", i)
			} else if err != nil {
				return err
			}
			switch lot.Status {
			case "active":
			case "blocked", "recalled":
				if toType != "quarantine" {
					return domain.Invalid("lines[%d]: lot %s %s durumunda; yalnizca karantina deposuna tasinabilir (LOT_BLOCKED)", i, lot.LotNo, lot.Status)
				}
			default:
				return domain.Invalid("lines[%d]: lot %s %s durumunda, transfer edilemez", i, lot.LotNo, lot.Status)
			}
			var onHand domain.Qty
			if err := tx.QueryRowContext(ctx,
				`SELECT COALESCE(SUM(qty), 0) FROM stock_movement WHERE lot_id = ? AND warehouse_id = ?`,
				line.LotID, req.FromWarehouseID).Scan(&onHand); err != nil {
				return err
			}
			if onHand.LessThan(line.Qty.Decimal) {
				return domain.Conflict("lines[%d]: kaynak depoda stok yetersiz (lot %s, eldeki %s, istenen %s, INSUFFICIENT_STOCK)",
					i, lot.LotNo, onHand.StringFixed(domain.QtyScale), line.Qty.StringFixed(domain.QtyScale))
			}
			for _, m := range []domain.StockMovement{
				{LotID: line.LotID, WarehouseID: req.FromWarehouseID, MovementType: "transfer_out", Qty: domain.NewQty(line.Qty.Decimal.Neg())},
				{LotID: line.LotID, WarehouseID: req.ToWarehouseID, MovementType: "transfer_in", Qty: line.Qty},
			} {
				m.UnitCost, m.RefType, m.RefID, m.Note = lot.UnitCost, "stock_transfer", id, req.Note
				if _, err := insertMovement(ctx, tx, m); err != nil {
					return err
				}
			}
			lotView, err := getLot(ctx, tx, line.LotID, true)
			if err != nil {
				return err
			}
			if err := appendChange(ctx, tx, "lot", line.LotID, "upsert", lotView); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetStockTransfer(ctx, id)
}
