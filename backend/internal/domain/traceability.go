package domain

import (
	"strings"
	"time"
)

// İzlenebilirlik (ileri/geri/genealogy) ve geri çağırma (Görev 3).
//
// KRİTİK: bu dosyadaki hiçbir sorgu/hesap ML'e dayanmaz; tüm sonuçlar lot_link (kesin kayıt
// ilişkileri) üzerinden recursive CTE ile hesaplanır (bkz. repo/traceability.go).

var (
	RecallStatuses        = set("open", "closed")
	RecallContactStatuses = set("pending", "contacted", "confirmed")
)

// İkinci kademe dağıtım (marketin kendi alıcılarına dağıttığı miktar) YALNIZCA kaydedilmişse
// (downstream_distribution, POST /downstream-distributions) kesin olarak gösterilir. Kayıt yoksa
// DownstreamKnown=false ve DownstreamNoteText döner; uydurma bir rakam üretilmez. Kayıt var ama
// müşteriye giden net miktarı karşılamıyorsa kayıtlı kısım gösterilir, kalan için
// DownstreamPartialNoteText döner.
const DownstreamNoteText = "İkinci kademe dağıtım kaydı yok, market ile iletişime geçilmeli"

const DownstreamPartialNoteText = "Kayıtlı ikinci kademe dağıtım gösteriliyor; kaydedilmeyen kalan miktar için market ile iletişime geçilmeli"

// ---------------------------------------------------------------------------
// trace-forward
// ---------------------------------------------------------------------------

type TraceShipmentLine struct {
	ShipmentNo   string    `json:"shipment_no"`
	CustomerID   string    `json:"customer_id"`
	CustomerName string    `json:"customer_name"`
	Qty          Qty       `json:"qty"`
	ShippedAt    time.Time `json:"shipped_at"`
}

type TraceSaleLine struct {
	OrderNo         string    `json:"order_no"`
	CustomerID      string    `json:"customer_id"`
	CustomerName    string    `json:"customer_name"`
	Qty             Qty       `json:"qty"`
	SoldAt          time.Time `json:"sold_at"`
	RecallViolation bool      `json:"recall_violation"`
}

// TraceForwardNode: trace-forward'daki tek bir düğüm (kök dahil, depth=0 kök). ParentLotID/
// QtyFromParent/ProcessType/ProcessID kökte null'dur (kökün bir ebeveyni yoktur bu izlemede).
type TraceForwardNode struct {
	LotID            string              `json:"lot_id"`
	LotNo            string              `json:"lot_no"`
	ItemID           string              `json:"item_id"`
	ItemName         string              `json:"item_name"`
	UOM              string              `json:"uom"`
	Status           string              `json:"status"`
	Depth            int                 `json:"depth"`
	Path             string              `json:"path"`
	ParentLotID      *string             `json:"parent_lot_id"`
	QtyFromParent    *Qty                `json:"qty_from_parent"`
	ProcessType      *string             `json:"process_type"`
	ProcessID        *string             `json:"process_id"`
	TotalProducedQty Qty                 `json:"total_produced_qty"`
	RatioFromParent  *string             `json:"ratio_from_parent"`
	CurrentStock     []LotBalance        `json:"current_stock"`
	QtyOnHandTotal   Qty                 `json:"qty_on_hand_total"`
	Shipments        []TraceShipmentLine `json:"shipments"`
	Sales            []TraceSaleLine     `json:"sales"`
	ReturnedQty      Qty                 `json:"returned_qty"`
}

type UomQty struct {
	UOM string `json:"uom"`
	Qty Qty    `json:"qty"`
}

type TraceForwardCustomerSummary struct {
	CustomerID      string  `json:"customer_id"`
	CustomerName    string  `json:"customer_name"`
	QtyShipped      Qty     `json:"qty_shipped"`
	QtyReturned     Qty     `json:"qty_returned"`
	DownstreamKnown bool    `json:"downstream_known"`
	DownstreamNote  *string `json:"downstream_note,omitempty"`
	// Downstream: kayıtlı ikinci kademe dağıtımlar (etkilenen lotlardan; kayıt yoksa boş).
	Downstream []DownstreamDistribution `json:"downstream"`
}

type TraceForwardSummary struct {
	AffectedLotCount int                           `json:"affected_lot_count"`
	RemainingStock   []UomQty                      `json:"remaining_stock"`
	Customers        []TraceForwardCustomerSummary `json:"customers"`
}

type TraceForwardResponse struct {
	RootLotID string              `json:"root_lot_id"`
	RootLotNo string              `json:"root_lot_no"`
	Nodes     []TraceForwardNode  `json:"nodes"`
	Summary   TraceForwardSummary `json:"summary"`
}

// ---------------------------------------------------------------------------
// trace-backward
// ---------------------------------------------------------------------------

// TraceBackwardNode: tek bir hammadde/ebeveyn lotu + bu düğümün hangi çocuğa hangi işlemle
// bağlandığı (process_type/process_id/order_no).
type TraceBackwardNode struct {
	LotID                string  `json:"lot_id"`
	LotNo                string  `json:"lot_no"`
	ItemID               string  `json:"item_id"`
	ItemName             string  `json:"item_name"`
	UOM                  string  `json:"uom"`
	SupplierID           *string `json:"supplier_id"`
	SupplierName         *string `json:"supplier_name"`
	SupplierContactName  *string `json:"supplier_contact_name"`
	SupplierPhone        *string `json:"supplier_phone"`
	SupplierEmail        *string `json:"supplier_email"`
	ReceivedOrProducedAt Date    `json:"received_or_produced_at"`
	ExpiryDate           *Date   `json:"expiry_date"`
	Depth                int     `json:"depth"`
	Path                 string  `json:"path"`
	ChildLotID           *string `json:"child_lot_id"`
	QtyUsed              *Qty    `json:"qty_used"`
	ProcessType          *string `json:"process_type"`
	ProcessID            *string `json:"process_id"`
	OrderNo              *string `json:"order_no"`
}

type TraceBackwardSummary struct {
	RawLotCount   int `json:"raw_lot_count"`
	SupplierCount int `json:"supplier_count"`
}

type TraceBackwardResponse struct {
	RootLotID string               `json:"root_lot_id"`
	RootLotNo string               `json:"root_lot_no"`
	Nodes     []TraceBackwardNode  `json:"nodes"`
	Summary   TraceBackwardSummary `json:"summary"`
}

// ---------------------------------------------------------------------------
// genealogy (tek çağrıda yukarı + aşağı)
// ---------------------------------------------------------------------------

type GenealogyNode struct {
	LotID    string `json:"lot_id"`
	LotNo    string `json:"lot_no"`
	ItemName string `json:"item_name"`
	// Level: negatif = yukarı (hammadde), 0 = kök, pozitif = aşağı (mamul).
	Level  int    `json:"level"`
	Status string `json:"status"`
}

type GenealogyEdge struct {
	FromLotID   string `json:"from_lot_id"`
	ToLotID     string `json:"to_lot_id"`
	Qty         Qty    `json:"qty"`
	UOM         string `json:"uom"`
	ProcessType string `json:"process_type"`
	ProcessID   string `json:"process_id"`
}

type GenealogyResponse struct {
	RootLotID string          `json:"root_lot_id"`
	RootLotNo string          `json:"root_lot_no"`
	Nodes     []GenealogyNode `json:"nodes"`
	Edges     []GenealogyEdge `json:"edges"`
}

// ---------------------------------------------------------------------------
// Geri çağırma
// ---------------------------------------------------------------------------

type RecallLot struct {
	LotID    string `json:"lot_id"`
	LotNo    string `json:"lot_no"`
	ItemName string `json:"item_name"`
	Depth    int    `json:"depth"`
	Path     string `json:"path"`
	Status   string `json:"status"`
}

type RecallCustomerView struct {
	ID              string  `json:"id"`
	CustomerID      string  `json:"customer_id"`
	CustomerName    string  `json:"customer_name"`
	CustomerPhone   *string `json:"customer_phone"`
	CustomerEmail   *string `json:"customer_email"`
	LotID           string  `json:"lot_id"`
	LotNo           string  `json:"lot_no"`
	QtyShipped      Qty     `json:"qty_shipped"`
	QtyReturned     Qty     `json:"qty_returned"`
	RemainingRisk   Qty     `json:"remaining_risk_qty"`
	ContactStatus   string  `json:"contact_status"`
	Notes           *string `json:"notes"`
	DownstreamKnown bool    `json:"downstream_known"`
	DownstreamNote  *string `json:"downstream_note,omitempty"`
	HasViolation    bool    `json:"has_violation"`
	// Downstream: bu müşterinin bu lottan kayıtlı ikinci kademe dağıtımları (kayıt yoksa boş).
	Downstream []DownstreamDistribution `json:"downstream"`
}

// Recall: GET/POST /recalls API JSON'u. AffectedLots/Customers yalnızca detay görünümünde
// (GET /recalls/{id}) doludur; liste görünümünde yalnızca sayaçlar (AffectedLotCount/
// PendingCustomerCount) doludur.
type Recall struct {
	ID                   string               `json:"id"`
	RecallNo             string               `json:"recall_no"`
	RootLotID            string               `json:"root_lot_id"`
	RootLotNo            string               `json:"root_lot_no"`
	Reason               string               `json:"reason"`
	Status               string               `json:"status"`
	OpenedAt             time.Time            `json:"opened_at"`
	ClosedAt             *time.Time           `json:"closed_at"`
	CreatedAt            time.Time            `json:"created_at"`
	AffectedLotCount     int                  `json:"affected_lot_count"`
	PendingCustomerCount int                  `json:"pending_customer_count"`
	AffectedLots         []RecallLot          `json:"affected_lots,omitempty"`
	Customers            []RecallCustomerView `json:"customers,omitempty"`
}

// CreateRecallRequest POST /recalls gövdesi.
type CreateRecallRequest struct {
	RootLotID string `json:"root_lot_id"`
	Reason    string `json:"reason"`
}

func (r *CreateRecallRequest) Validate() error {
	if _, err := ParseID(r.RootLotID); err != nil {
		return Invalid("root_lot_id: %s", err)
	}
	r.Reason = strings.TrimSpace(r.Reason)
	return required("reason", r.Reason, 400)
}

// PatchRecallCustomerRequest PATCH /recalls/{id}/customers/{recall_customer_id} gövdesi.
type PatchRecallCustomerRequest struct {
	ContactStatus *string `json:"contact_status"`
	Notes         *string `json:"notes"`
}

func (r *PatchRecallCustomerRequest) Validate() error {
	if r.ContactStatus != nil {
		if err := oneOf("contact_status", *r.ContactStatus, RecallContactStatuses); err != nil {
			return err
		}
	}
	trimPtr(&r.Notes)
	if r.Notes != nil && len(*r.Notes) > 400 {
		return Invalid("notes en fazla 400 karakter olabilir")
	}
	if r.ContactStatus == nil && r.Notes == nil {
		return Invalid("contact_status veya notes verilmeli")
	}
	return nil
}
