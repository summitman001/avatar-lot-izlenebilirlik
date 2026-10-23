package domain

import (
	"encoding/json"
	"strings"
	"time"
)

// Varliklar hem API JSON'u hem sync change_log payload'i hem de repo satiridir.
// Create/Update istek govdesi de ayni yapidir; id bos birakilirsa sunucu UUIDv7 uretir,
// verilirse (istemci tarafi uretim) UUIDv7 olmak zorundadir. created_at istekte yok sayilir.

var (
	itemTypes      = set("raw", "finished", "cut_part", "by_product", "waste")
	uoms           = set("kg", "adet")
	supplierTypes  = set("material", "external_producer", "livestock")
	customerTypes  = set("market", "wholesale", "end_customer")
	warehouseTypes = set("internal", "external_producer", "quarantine", "vehicle")
	LotStatuses    = set("active", "blocked", "recalled", "consumed")
)

func set(v ...string) map[string]bool {
	m := make(map[string]bool, len(v))
	for _, s := range v {
		m[s] = true
	}
	return m
}

func oneOf(field, v string, allowed map[string]bool) error {
	if !allowed[v] {
		return Invalid("%s gecersiz: %q", field, v)
	}
	return nil
}

func required(field, v string, max int) error {
	if v == "" {
		return Invalid("%s zorunlu", field)
	}
	if len(v) > max {
		return Invalid("%s en fazla %d karakter olabilir", field, max)
	}
	return nil
}

// trimPtr bos string'i nil'e cevirir.
func trimPtr(p **string) {
	if *p == nil {
		return
	}
	s := strings.TrimSpace(**p)
	if s == "" {
		*p = nil
		return
	}
	*p = &s
}

func checkEmail(p *string) error {
	if p != nil && !strings.Contains(*p, "@") {
		return Invalid("email gecersiz: %q", *p)
	}
	return nil
}

type Item struct {
	ID               string    `json:"id"`
	Code             string    `json:"code"`
	Name             string    `json:"name"`
	ItemType         string    `json:"item_type"`
	BaseUOM          string    `json:"base_uom"`
	UnitWeightKg     *Qty      `json:"unit_weight_kg"`
	DefaultSalePrice *Amount   `json:"default_sale_price"`
	IsActive         bool      `json:"is_active"`
	CreatedAt        time.Time `json:"created_at"`
}

// UnmarshalJSON: is_active gonderilmezse true kabul edilir (yeni urun varsayilan olarak aktif).
func (i *Item) UnmarshalJSON(b []byte) error {
	type plain Item
	p := plain{IsActive: true}
	err := json.Unmarshal(b, &p)
	*i = Item(p)
	return err
}

func (i *Item) Validate() error {
	i.Code, i.Name = strings.TrimSpace(i.Code), strings.TrimSpace(i.Name)
	if err := required("code", i.Code, 40); err != nil {
		return err
	}
	if err := required("name", i.Name, 160); err != nil {
		return err
	}
	if err := oneOf("item_type", i.ItemType, itemTypes); err != nil {
		return err
	}
	if err := oneOf("base_uom", i.BaseUOM, uoms); err != nil {
		return err
	}
	if i.UnitWeightKg != nil && !i.UnitWeightKg.IsPositive() {
		return Invalid("unit_weight_kg sifirdan buyuk olmali")
	}
	if i.DefaultSalePrice != nil && i.DefaultSalePrice.IsNegative() {
		return Invalid("default_sale_price negatif olamaz")
	}
	return nil
}

// Party: tedarikci ve musteri ortak iletisim alanlari.
type Supplier struct {
	ID           string    `json:"id"`
	Code         string    `json:"code"`
	Name         string    `json:"name"`
	SupplierType string    `json:"supplier_type"`
	ContactName  *string   `json:"contact_name"`
	Phone        *string   `json:"phone"`
	Email        *string   `json:"email"`
	Address      *string   `json:"address"`
	CreatedAt    time.Time `json:"created_at"`
}

func (s *Supplier) Validate() error {
	s.Code, s.Name = strings.TrimSpace(s.Code), strings.TrimSpace(s.Name)
	if err := required("code", s.Code, 40); err != nil {
		return err
	}
	if err := required("name", s.Name, 160); err != nil {
		return err
	}
	if err := oneOf("supplier_type", s.SupplierType, supplierTypes); err != nil {
		return err
	}
	trimPtr(&s.ContactName)
	trimPtr(&s.Phone)
	trimPtr(&s.Email)
	trimPtr(&s.Address)
	return checkEmail(s.Email)
}

type Customer struct {
	ID           string    `json:"id"`
	Code         string    `json:"code"`
	Name         string    `json:"name"`
	CustomerType string    `json:"customer_type"`
	ContactName  *string   `json:"contact_name"`
	Phone        *string   `json:"phone"`
	Email        *string   `json:"email"`
	Address      *string   `json:"address"`
	CreatedAt    time.Time `json:"created_at"`
}

func (c *Customer) Validate() error {
	c.Code, c.Name = strings.TrimSpace(c.Code), strings.TrimSpace(c.Name)
	if err := required("code", c.Code, 40); err != nil {
		return err
	}
	if err := required("name", c.Name, 160); err != nil {
		return err
	}
	if err := oneOf("customer_type", c.CustomerType, customerTypes); err != nil {
		return err
	}
	trimPtr(&c.ContactName)
	trimPtr(&c.Phone)
	trimPtr(&c.Email)
	trimPtr(&c.Address)
	return checkEmail(c.Email)
}

type Warehouse struct {
	ID            string    `json:"id"`
	Code          string    `json:"code"`
	Name          string    `json:"name"`
	WarehouseType string    `json:"warehouse_type"`
	CreatedAt     time.Time `json:"created_at"`
}

func (w *Warehouse) Validate() error {
	w.Code, w.Name = strings.TrimSpace(w.Code), strings.TrimSpace(w.Name)
	if err := required("code", w.Code, 40); err != nil {
		return err
	}
	if err := required("name", w.Name, 160); err != nil {
		return err
	}
	return oneOf("warehouse_type", w.WarehouseType, warehouseTypes)
}

// ---------------------------------------------------------------------------
// Lot ve stok
// ---------------------------------------------------------------------------

// LotPurchase: alis lotunun belge bilgisi. has_mismatch=true ise belgede beyan edilen tutar
// hesaplanandan farklidir (ornek KZ-2026-001: 287.50 hesaplanan, 351.00 beyan).
type LotPurchase struct {
	ReceiptID         string  `json:"receipt_id"`
	DocNo             string  `json:"doc_no"`
	InvoiceNo         *string `json:"invoice_no"`
	UnitPrice         Amount  `json:"unit_price"`
	LineTotal         Amount  `json:"line_total"`
	DeclaredLineTotal *Amount `json:"declared_line_total"`
	HasMismatch       bool    `json:"has_mismatch"`
}

type LotBalance struct {
	WarehouseID   string `json:"warehouse_id"`
	WarehouseCode string `json:"warehouse_code"`
	QtyOnHand     Qty    `json:"qty_on_hand"`
}

type Lot struct {
	ID                   string       `json:"id"`
	LotNo                string       `json:"lot_no"`
	ItemID               string       `json:"item_id"`
	ItemCode             string       `json:"item_code"`
	ItemName             string       `json:"item_name"`
	SupplierID           *string      `json:"supplier_id"`
	SupplierName         *string      `json:"supplier_name"`
	OriginType           string       `json:"origin_type"`
	OriginID             *string      `json:"origin_id"`
	ReceivedOrProducedAt Date         `json:"received_or_produced_at"`
	ExpiryDate           *Date        `json:"expiry_date"`
	InitialQty           Qty          `json:"initial_qty"`
	UOM                  string       `json:"uom"`
	UnitCost             Amount       `json:"unit_cost"`
	Status               string       `json:"status"`
	BlockedReason        *string      `json:"blocked_reason"`
	CreatedAt            time.Time    `json:"created_at"`
	Purchase             *LotPurchase `json:"purchase"`
	// Yalnizca GET /lots ve /lots/{id}: tum depolardaki toplam ve depo kirilimi.
	QtyOnHand *Qty         `json:"qty_on_hand,omitempty"`
	Balances  []LotBalance `json:"balances,omitempty"`
}

// PurchaseLotRequest POST /lots govdesi: bir alis lotu + purchase_in hareketi olusturur.
type PurchaseLotRequest struct {
	ID                *string `json:"id"` // opsiyonel istemci UUIDv7'si
	LotNo             string  `json:"lot_no"`
	ItemID            string  `json:"item_id"`
	SupplierID        string  `json:"supplier_id"`
	WarehouseID       string  `json:"warehouse_id"`
	ReceivedAt        Date    `json:"received_at"`
	ExpiryDate        *Date   `json:"expiry_date"`
	Qty               Qty     `json:"qty"`
	UnitPrice         Amount  `json:"unit_price"`
	DeclaredLineTotal *Amount `json:"declared_line_total"` // belgede yazan tutar (opsiyonel)
	DocNo             *string `json:"doc_no"`              // varsayilan "PR-<lot_no>"; ayni belge no + tedarikci => ayni belge
	InvoiceNo         *string `json:"invoice_no"`
}

func (r *PurchaseLotRequest) Validate() error {
	r.LotNo = strings.TrimSpace(r.LotNo)
	if err := required("lot_no", r.LotNo, 40); err != nil {
		return err
	}
	for field, id := range map[string]string{"item_id": r.ItemID, "supplier_id": r.SupplierID, "warehouse_id": r.WarehouseID} {
		if _, err := ParseID(id); err != nil {
			return Invalid("%s: %s", field, err)
		}
	}
	if r.ID != nil {
		if _, err := ParseID(*r.ID); err != nil {
			return Invalid("id: %s", err)
		}
	}
	if r.ReceivedAt.IsZero() {
		return Invalid("received_at zorunlu")
	}
	if !r.Qty.IsPositive() {
		return Invalid("qty sifirdan buyuk olmali")
	}
	if r.UnitPrice.IsNegative() {
		return Invalid("unit_price negatif olamaz")
	}
	if r.DeclaredLineTotal != nil && r.DeclaredLineTotal.IsNegative() {
		return Invalid("declared_line_total negatif olamaz")
	}
	if r.ExpiryDate != nil && r.ExpiryDate.Before(r.ReceivedAt.Time) {
		return Invalid("expiry_date received_at tarihinden once olamaz")
	}
	trimPtr(&r.DocNo)
	trimPtr(&r.InvoiceNo)
	return nil
}

type StockMovement struct {
	ID                 string     `json:"id"`
	LotID              string     `json:"lot_id"`
	LotNo              string     `json:"lot_no"`
	WarehouseID        string     `json:"warehouse_id"`
	MovementType       string     `json:"movement_type"`
	Qty                Qty        `json:"qty"` // isaretli: giris +, cikis -
	UnitCost           Amount     `json:"unit_cost"`
	RefType            string     `json:"ref_type"`
	RefID              string     `json:"ref_id"`
	ReversesMovementID *string    `json:"reverses_movement_id"`
	DeviceID           *string    `json:"device_id"`
	ClientCreatedAt    *time.Time `json:"client_created_at"`
	Note               *string    `json:"note"`
	CreatedAt          time.Time  `json:"created_at"`
}

// ReverseRequest POST /stock-movements/{id}/reverse govdesi (hepsi opsiyonel).
type ReverseRequest struct {
	ID       *string `json:"id"` // istemci uretimli UUIDv7 (offline idempotency)
	Note     *string `json:"note"`
	DeviceID *string `json:"device_id"`
}

func (r *ReverseRequest) Validate() error {
	if r.ID != nil {
		if _, err := ParseID(*r.ID); err != nil {
			return Invalid("id: %s", err)
		}
	}
	if r.DeviceID != nil {
		if _, err := ParseID(*r.DeviceID); err != nil {
			return Invalid("device_id: %s", err)
		}
	}
	trimPtr(&r.Note)
	return nil
}
