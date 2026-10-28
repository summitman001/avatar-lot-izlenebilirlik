package http

import (
	nethttp "net/http"

	"github.com/go-chi/chi/v5"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/repo"
)

// ---------------------------------------------------------------------------
// alis fisleri (purchase-receipts)
// ---------------------------------------------------------------------------

func (h *handlers) listPurchaseReceipts(w nethttp.ResponseWriter, r *nethttp.Request) {
	p, err := pageFrom(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	mismatch, err := boolParam(r, "has_mismatch")
	if err != nil {
		writeError(w, r, err)
		return
	}
	q := r.URL.Query()
	items, err := h.store.ListPurchaseReceipts(r.Context(), repo.PurchaseReceiptFilter{
		SupplierID: q.Get("supplier_id"), ItemID: q.Get("item_id"),
		From: q.Get("from"), To: q.Get("to"), HasMismatch: mismatch, Page: p,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]any{"items": items})
}

func (h *handlers) getPurchaseReceipt(w nethttp.ResponseWriter, r *nethttp.Request) {
	out, err := h.store.GetPurchaseReceipt(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, out)
}

// createPurchaseReceipt: belge + N satir (her biri bir lot + purchase_in hareketi) tek transaction'da
// olusturur (bkz. repo/purchase.go CreatePurchaseReceipt, repo/lots.go createPurchaseLotInTx).
func (h *handlers) createPurchaseReceipt(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req domain.CreatePurchaseReceiptRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := req.Validate(); err != nil {
		writeError(w, r, err)
		return
	}
	out, err := h.store.CreatePurchaseReceipt(r.Context(), req)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusCreated, out)
}

// ---------------------------------------------------------------------------
// tahminler (predictions) -- bkz. docs/ml-report.md
// ---------------------------------------------------------------------------

var predictionTargetTypes = map[string]bool{"production": true, "cutting": true}

func (h *handlers) listPredictions(w nethttp.ResponseWriter, r *nethttp.Request) {
	p, err := pageFrom(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	q := r.URL.Query()
	if tt := q.Get("target_type"); tt != "" && !predictionTargetTypes[tt] {
		writeError(w, r, badRequest("target_type gecersiz: "+tt))
		return
	}
	hasActual, err := boolParam(r, "has_actual")
	if err != nil {
		writeError(w, r, err)
		return
	}
	items, err := h.store.ListPredictions(r.Context(), repo.PredictionFilter{
		TargetType: q.Get("target_type"), ModelName: q.Get("model_name"),
		From: q.Get("from"), To: q.Get("to"), HasActual: hasActual, Page: p,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]any{"items": items})
}

func (h *handlers) predictionsSummary(w nethttp.ResponseWriter, r *nethttp.Request) {
	out, err := h.store.GetPredictionsSummary(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, out)
}
