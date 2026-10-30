package domain

import (
	"strings"
	"time"
)

// Package domain (lot_ops.go): Gorev 3 "yeniden isleme ve fire hareketleri lot bazinda kaydedilir" ve
// "ikinci kademe dagitim yalnizca kaydedilmisse kesin olarak gosterilir" maddelerinin istek/gorunum
// tipleri (bkz. repo/lot_ops.go, docs/traceability.md).

func normalizeIDPtr(field string, p *string) (*string, error) {
	if p == nil {
		return nil, nil
	}
	id, err := ParseID(*p)
	if err != nil {
		return nil, Invalid("%s: %s", field, err)
	}
	return &id, nil
}

// ---------------------------------------------------------------------------
// fire (POST /lots/{id}/waste)
// ---------------------------------------------------------------------------

// WasteRequest bir lotun belirli bir depodaki miktarini fire olarak dusurur (waste_out hareketi).
// id verilirse hareketin id'si olur (idempotency).
type WasteRequest struct {
	ID          *string `json:"id"`
	WarehouseID string  `json:"warehouse_id"`
	Qty         Qty     `json:"qty"`
	Reason      string  `json:"reason"`
}

func (r *WasteRequest) Validate() error {
	var err error
	if r.ID, err = normalizeIDPtr("id", r.ID); err != nil {
		return err
	}
	if _, err := ParseID(r.WarehouseID); err != nil {
		return Invalid("warehouse_id: %s", err)
	}
	if !r.Qty.IsPositive() {
		return Invalid("qty sifirdan buyuk olmali")
	}
	r.Reason = strings.TrimSpace(r.Reason)
	return required("reason", r.Reason, 400)
}

// ---------------------------------------------------------------------------
// yeniden isleme (POST /reprocess-orders)
// ---------------------------------------------------------------------------

type ReprocessInputRequest struct {
	LotID string `json:"lot_id"`
	Qty   Qty    `json:"qty"`
}

type ReprocessOutputRequest struct {
	ItemID     string  `json:"item_id"`
	Qty        Qty     `json:"qty"`
	LotNo      *string `json:"lot_no"` // bos ise YIL-<yil>-NNN uretilir
	ExpiryDate *Date   `json:"expiry_date"`
}

// CreateReprocessOrderRequest: girdi lotlari warehouse_id'den tuketilir, cikti lotu ayni depoya girer.
type CreateReprocessOrderRequest struct {
	ID          *string                 `json:"id"`
	WarehouseID string                  `json:"warehouse_id"`
	Inputs      []ReprocessInputRequest `json:"inputs"`
	Output      ReprocessOutputRequest  `json:"output"`
	Note        *string                 `json:"note"`
}

func (r *CreateReprocessOrderRequest) Validate() error {
	var err error
	if r.ID, err = normalizeIDPtr("id", r.ID); err != nil {
		return err
	}
	if _, err := ParseID(r.WarehouseID); err != nil {
		return Invalid("warehouse_id: %s", err)
	}
	if len(r.Inputs) == 0 {
		return Invalid("en az bir girdi (inputs) gerekli")
	}
	seen := map[string]bool{}
	for i, in := range r.Inputs {
		id, err := ParseID(in.LotID)
		if err != nil {
			return Invalid("inputs[%d].lot_id: %s", i, err)
		}
		if seen[id] {
			return Invalid("inputs[%d]: ayni lot birden fazla satirda", i)
		}
		seen[id] = true
		if !in.Qty.IsPositive() {
			return Invalid("inputs[%d].qty sifirdan buyuk olmali", i)
		}
	}
	if _, err := ParseID(r.Output.ItemID); err != nil {
		return Invalid("output.item_id: %s", err)
	}
	if !r.Output.Qty.IsPositive() {
		return Invalid("output.qty sifirdan buyuk olmali")
	}
	trimPtr(&r.Output.LotNo)
	if r.Output.LotNo != nil && len(*r.Output.LotNo) > 40 {
		return Invalid("output.lot_no en fazla 40 karakter olabilir")
	}
	trimPtr(&r.Note)
	if r.Note != nil && len(*r.Note) > 400 {
		return Invalid("note en fazla 400 karakter olabilir")
	}
	return nil
}

type ReprocessInputLine struct {
	LotID    string `json:"lot_id"`
	LotNo    string `json:"lot_no"`
	ItemName string `json:"item_name"`
	Qty      Qty    `json:"qty"`
	UOM      string `json:"uom"`
	UnitCost Amount `json:"unit_cost"`
}

type ReprocessOrder struct {
	ID            string               `json:"id"`
	OrderNo       string               `json:"order_no"`
	WarehouseID   string               `json:"warehouse_id"`
	WarehouseCode string               `json:"warehouse_code"`
	OutputLotID   *string              `json:"output_lot_id"`
	OutputLotNo   *string              `json:"output_lot_no"`
	OutputItem    *string              `json:"output_item_name"`
	OutputQty     *Qty                 `json:"output_qty"`
	OutputUOM     *string              `json:"output_uom"`
	OutputUnit    *Amount              `json:"output_unit_cost"`
	TotalCost     Amount               `json:"total_cost"`
	Note          *string              `json:"note"`
	CreatedAt     time.Time            `json:"created_at"`
	Inputs        []ReprocessInputLine `json:"inputs"`
}

// ---------------------------------------------------------------------------
// ikinci kademe dagitim (POST/GET /downstream-distributions)
// ---------------------------------------------------------------------------

type CreateDownstreamRequest struct {
	ID             *string `json:"id"`
	CustomerID     string  `json:"customer_id"`
	LotID          string  `json:"lot_id"`
	RecipientName  string  `json:"recipient_name"`
	RecipientPhone *string `json:"recipient_phone"`
	Qty            Qty     `json:"qty"`
	DistributedAt  Date    `json:"distributed_at"`
	Note           *string `json:"note"`
}

func (r *CreateDownstreamRequest) Validate() error {
	var err error
	if r.ID, err = normalizeIDPtr("id", r.ID); err != nil {
		return err
	}
	for field, id := range map[string]string{"customer_id": r.CustomerID, "lot_id": r.LotID} {
		if _, err := ParseID(id); err != nil {
			return Invalid("%s: %s", field, err)
		}
	}
	r.RecipientName = strings.TrimSpace(r.RecipientName)
	if err := required("recipient_name", r.RecipientName, 160); err != nil {
		return err
	}
	trimPtr(&r.RecipientPhone)
	if r.RecipientPhone != nil && len(*r.RecipientPhone) > 40 {
		return Invalid("recipient_phone en fazla 40 karakter olabilir")
	}
	if !r.Qty.IsPositive() {
		return Invalid("qty sifirdan buyuk olmali")
	}
	if r.DistributedAt.IsZero() {
		return Invalid("distributed_at zorunlu")
	}
	trimPtr(&r.Note)
	if r.Note != nil && len(*r.Note) > 400 {
		return Invalid("note en fazla 400 karakter olabilir")
	}
	return nil
}

// DownstreamDistribution kayitli bir ikinci kademe dagitim satiri.
type DownstreamDistribution struct {
	ID             string    `json:"id"`
	CustomerID     string    `json:"customer_id"`
	CustomerName   string    `json:"customer_name"`
	LotID          string    `json:"lot_id"`
	LotNo          string    `json:"lot_no"`
	RecipientName  string    `json:"recipient_name"`
	RecipientPhone *string   `json:"recipient_phone"`
	Qty            Qty       `json:"qty"`
	DistributedAt  Date      `json:"distributed_at"`
	Note           *string   `json:"note"`
	CreatedAt      time.Time `json:"created_at"`
}
