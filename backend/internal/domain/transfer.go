package domain

import (
	"strings"
	"time"
)

// Package domain (transfer.go): POST/GET /stock-transfers -- depolar arasi stok aktarimi.

type StockTransferLineRequest struct {
	LotID string `json:"lot_id"`
	Qty   Qty    `json:"qty"`
}

// CreateStockTransferRequest POST /stock-transfers govdesi. id verilirse idempotency anahtaridir.
type CreateStockTransferRequest struct {
	ID              *string                    `json:"id"`
	FromWarehouseID string                     `json:"from_warehouse_id"`
	ToWarehouseID   string                     `json:"to_warehouse_id"`
	Note            *string                    `json:"note"`
	Lines           []StockTransferLineRequest `json:"lines"`
}

func (r *CreateStockTransferRequest) Validate() error {
	if r.ID != nil {
		id, err := ParseID(*r.ID)
		if err != nil {
			return Invalid("id: %s", err)
		}
		r.ID = &id
	}
	for field, id := range map[string]string{"from_warehouse_id": r.FromWarehouseID, "to_warehouse_id": r.ToWarehouseID} {
		if _, err := ParseID(id); err != nil {
			return Invalid("%s: %s", field, err)
		}
	}
	if strings.EqualFold(r.FromWarehouseID, r.ToWarehouseID) {
		return Invalid("kaynak ve hedef depo ayni olamaz")
	}
	trimPtr(&r.Note)
	if len(r.Lines) == 0 {
		return Invalid("en az bir satir (lines) gerekli")
	}
	seen := map[string]bool{}
	for i, l := range r.Lines {
		if _, err := ParseID(l.LotID); err != nil {
			return Invalid("lines[%d].lot_id: %s", i, err)
		}
		if seen[strings.ToLower(l.LotID)] {
			return Invalid("lines[%d]: ayni lot birden fazla satirda", i)
		}
		seen[strings.ToLower(l.LotID)] = true
		if !l.Qty.IsPositive() {
			return Invalid("lines[%d].qty sifirdan buyuk olmali", i)
		}
	}
	return nil
}

type StockTransferLine struct {
	LotID  string `json:"lot_id"`
	LotNo  string `json:"lot_no"`
	ItemID string `json:"item_id"`
	Qty    Qty    `json:"qty"`
}

// StockTransfer GET /stock-transfers(/{id}) gorunumu; satirlar transfer_in hareketlerinden okunur.
type StockTransfer struct {
	ID                string              `json:"id"`
	TransferNo        string              `json:"transfer_no"`
	FromWarehouseID   string              `json:"from_warehouse_id"`
	FromWarehouseCode string              `json:"from_warehouse_code"`
	ToWarehouseID     string              `json:"to_warehouse_id"`
	ToWarehouseCode   string              `json:"to_warehouse_code"`
	Note              *string             `json:"note"`
	CreatedAt         time.Time           `json:"created_at"`
	Lines             []StockTransferLine `json:"lines"`
}
