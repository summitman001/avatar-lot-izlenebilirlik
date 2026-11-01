package domain

import (
	"strings"
	"time"
)

// Satış/sevkiyat/iade (bu görev) + bloke lot mekanizması. Durum makinesi yok: bir satış oluşunca
// doğrudan "confirmed" olur (taslak/iptal akışı bu görevin kapsamında değil). Tek servis fonksiyonu
// (repo.Store.CreateSalesOrder) hem REST hem /sync/push tarafından kullanılır: "online" (REST) modda
// bloke lot/yetersiz stok sert hata (422/409) üretir; "offline" (sync) modda satış YİNE KAYDEDİLİR,
// ihlal bayrakları (Violations) ile "flagged" sonucu döner (bkz. docs/sync.md, "Offline kuralı").

const (
	SalesOrderConfirmed = "confirmed"

	// ViolationLotBlocked: offline satışta lot sunucuda blocked/recalled durumundaydı; satış yine
	// kaydedildi, sales_line.recall_violation=true.
	ViolationLotBlocked = "LOT_BLOCKED_AT_SALE"
	// ViolationNegativeStock: offline satışta bakiye yetersizdi; satış yine kaydedildi, stok negatife
	// düştü, anomaly_alert (critical) açıldı.
	ViolationNegativeStock = "NEGATIVE_STOCK_OFFLINE"
)

// ---------------------------------------------------------------------------
// Varlıklar
// ---------------------------------------------------------------------------

type SalesLine struct {
	ID              string  `json:"id"`
	SalesOrderID    string  `json:"sales_order_id"`
	ItemID          string  `json:"item_id"`
	ItemCode        string  `json:"item_code"`
	ItemName        string  `json:"item_name"`
	LotID           string  `json:"lot_id"`
	LotNo           string  `json:"lot_no"`
	Qty             Qty     `json:"qty"`
	UnitPrice       Amount  `json:"unit_price"`
	LineTotal       Amount  `json:"line_total"`
	StockMovementID *string `json:"stock_movement_id"`
	RecallViolation bool    `json:"recall_violation"`
	// NegativeStockViolation: offline satışta bakiye yetersizdi (bu satıra bağlı NEGATIVE_STOCK_OFFLINE
	// uyarısı var); satış kaydedildi, stok negatife düştü.
	NegativeStockViolation bool      `json:"negative_stock_violation"`
	CreatedAt              time.Time `json:"created_at"`
}

type ShipmentLine struct {
	ID     string `json:"id"`
	LotID  string `json:"lot_id"`
	LotNo  string `json:"lot_no"`
	ItemID string `json:"item_id"`
	Qty    Qty    `json:"qty"`
}

// Shipment: bir satış siparişine bağlı (veya bağımsız) sevkiyat. "hangi markete kaç paket gitti"
// sorusu Lines üzerinden (lot_id + qty) cevaplanır.
type Shipment struct {
	ID           string         `json:"id"`
	ShipmentNo   string         `json:"shipment_no"`
	CustomerID   string         `json:"customer_id"`
	CustomerName string         `json:"customer_name"`
	SalesOrderID *string        `json:"sales_order_id"`
	ShippedAt    time.Time      `json:"shipped_at"`
	Vehicle      *string        `json:"vehicle"`
	CreatedAt    time.Time      `json:"created_at"`
	Lines        []ShipmentLine `json:"lines,omitempty"`
}

// SalesOrder API JSON'udur. Violations yalnızca sync push sonucunda (flagged) doldurulur; GET
// yanıtlarında satır bazlı recall_violation / negative_stock_violation'a bakılmalıdır.
type SalesOrder struct {
	ID            string    `json:"id"`
	OrderNo       string    `json:"order_no"`
	CustomerID    string    `json:"customer_id"`
	CustomerCode  string    `json:"customer_code"`
	CustomerName  string    `json:"customer_name"`
	DeviceID      *string   `json:"device_id"`
	WarehouseID   string    `json:"warehouse_id"`
	WarehouseCode string    `json:"warehouse_code"`
	SoldAt        time.Time `json:"sold_at"`
	Status        string    `json:"status"`
	TotalAmount   Amount    `json:"total_amount"`
	// HasViolation: en az bir satırda recall_violation veya negative_stock_violation (liste görünümünde
	// rozet için; detayda hangi satırın neyi ihlal ettiği Lines[i]'den okunur).
	HasViolation bool        `json:"has_violation"`
	CreatedAt    time.Time   `json:"created_at"`
	Lines        []SalesLine `json:"lines,omitempty"`
	Shipments    []Shipment  `json:"shipments,omitempty"`
}

type CustomerReturn struct {
	ID           string    `json:"id"`
	ReturnNo     string    `json:"return_no"`
	CustomerID   string    `json:"customer_id"`
	CustomerName string    `json:"customer_name"`
	SalesLineID  *string   `json:"sales_line_id"`
	LotID        string    `json:"lot_id"`
	LotNo        string    `json:"lot_no"`
	Qty          Qty       `json:"qty"`
	Reason       string    `json:"reason"`
	ReturnedAt   time.Time `json:"returned_at"`
	CreatedAt    time.Time `json:"created_at"`
}

// ---------------------------------------------------------------------------
// İstek gövdeleri
// ---------------------------------------------------------------------------

// CreateSalesOrderLine tek satış satırı (lot seçimi istemcidedir: yalnız aktif lotlar REST'ten
// listelenir; offline satışta cihaz kendi önbelleğindeki lotu seçer, sunucu durumu sonradan değerlendirir).
type CreateSalesOrderLine struct {
	ID        *string `json:"id"`
	ItemID    string  `json:"item_id"`
	LotID     string  `json:"lot_id"`
	Qty       Qty     `json:"qty"`
	UnitPrice Amount  `json:"unit_price"`
}

// CreateSalesOrderRequest POST /sales-orders (ve sync sales_order/create) gövdesi.
//
// create_shipment varsayılan true: satış ile birlikte aynı satırlardan (lot_id+qty) bir sevkiyat
// (shipment+shipment_line) oluşturulur. false verilirse yalnız satış/stok hareketi oluşur, sevkiyat
// ayrıca POST ile değil (bu görevde sevkiyat her zaman satışla birlikte doğar) sonra eklenemez; bu
// alan yalnızca "stoktan düş ama henüz sevk etme" senaryosu için opsiyoneldir.
type CreateSalesOrderRequest struct {
	ID             *string                `json:"id"`
	OrderNo        *string                `json:"order_no"` // boş ise SAT-YYYY-NNN atomik üretilir
	CustomerID     string                 `json:"customer_id"`
	WarehouseID    string                 `json:"warehouse_id"`
	DeviceID       *string                `json:"device_id"`
	SoldAt         time.Time              `json:"sold_at"`
	Lines          []CreateSalesOrderLine `json:"lines"`
	CreateShipment *bool                  `json:"create_shipment"`
}

func (r *CreateSalesOrderRequest) Validate() error {
	if _, err := ParseID(r.CustomerID); err != nil {
		return Invalid("customer_id: %s", err)
	}
	if _, err := ParseID(r.WarehouseID); err != nil {
		return Invalid("warehouse_id: %s", err)
	}
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
	trimPtr(&r.OrderNo)
	if r.OrderNo != nil && len(*r.OrderNo) > 40 {
		return Invalid("order_no en fazla 40 karakter olabilir")
	}
	if r.SoldAt.IsZero() {
		return Invalid("sold_at zorunlu")
	}
	if len(r.Lines) == 0 {
		return Invalid("lines bos olamaz")
	}
	for i, l := range r.Lines {
		if _, err := ParseID(l.ItemID); err != nil {
			return Invalid("lines[%d].item_id: %s", i, err)
		}
		if _, err := ParseID(l.LotID); err != nil {
			return Invalid("lines[%d].lot_id: %s", i, err)
		}
		if l.ID != nil {
			if _, err := ParseID(*l.ID); err != nil {
				return Invalid("lines[%d].id: %s", i, err)
			}
		}
		if !l.Qty.IsPositive() {
			return Invalid("lines[%d].qty sifirdan buyuk olmali", i)
		}
		if l.UnitPrice.IsNegative() {
			return Invalid("lines[%d].unit_price negatif olamaz", i)
		}
	}
	return nil
}

// CreateCustomerReturnRequest POST /customer-returns (ve sync customer_return/create) gövdesi.
// sales_line_id veya lot_id'den en az biri verilmelidir (ikisi de verilirse tutarlılığı repo katmanı
// kontrol eder: sales_line'ın lot_id'si ile eşleşmeli).
type CreateCustomerReturnRequest struct {
	ID          *string    `json:"id"`
	ReturnNo    *string    `json:"return_no"`
	CustomerID  string     `json:"customer_id"`
	SalesLineID *string    `json:"sales_line_id"`
	LotID       *string    `json:"lot_id"`
	Qty         Qty        `json:"qty"`
	Reason      string     `json:"reason"`
	ReturnedAt  *time.Time `json:"returned_at"` // bos ise sunucu aninda (now)
}

func (r *CreateCustomerReturnRequest) Validate() error {
	if _, err := ParseID(r.CustomerID); err != nil {
		return Invalid("customer_id: %s", err)
	}
	if r.ID != nil {
		if _, err := ParseID(*r.ID); err != nil {
			return Invalid("id: %s", err)
		}
	}
	if r.SalesLineID == nil && r.LotID == nil {
		return Invalid("sales_line_id veya lot_id verilmeli")
	}
	if r.SalesLineID != nil {
		if _, err := ParseID(*r.SalesLineID); err != nil {
			return Invalid("sales_line_id: %s", err)
		}
	}
	if r.LotID != nil {
		if _, err := ParseID(*r.LotID); err != nil {
			return Invalid("lot_id: %s", err)
		}
	}
	trimPtr(&r.ReturnNo)
	if r.ReturnNo != nil && len(*r.ReturnNo) > 40 {
		return Invalid("return_no en fazla 40 karakter olabilir")
	}
	if !r.Qty.IsPositive() {
		return Invalid("qty sifirdan buyuk olmali")
	}
	r.Reason = strings.TrimSpace(r.Reason)
	if err := required("reason", r.Reason, 400); err != nil {
		return err
	}
	return nil
}

// BlockLotRequest POST /lots/{id}/block gövdesi.
type BlockLotRequest struct {
	Reason string `json:"reason"`
}

func (r *BlockLotRequest) Validate() error {
	r.Reason = strings.TrimSpace(r.Reason)
	return required("reason", r.Reason, 400)
}
