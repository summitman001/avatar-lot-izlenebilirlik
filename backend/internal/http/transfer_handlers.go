package http

import (
	nethttp "net/http"

	"github.com/go-chi/chi/v5"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/repo"
)

// ---------------------------------------------------------------------------
// depolar arasi transfer (stock-transfers) -- bkz. repo/transfer.go
// ---------------------------------------------------------------------------

func (h *handlers) listStockTransfers(w nethttp.ResponseWriter, r *nethttp.Request) {
	p, err := pageFrom(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	q := r.URL.Query()
	items, err := h.store.ListStockTransfers(r.Context(), repo.StockTransferFilter{
		WarehouseID: q.Get("warehouse_id"), LotID: q.Get("lot_id"), Page: p,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]any{"items": items})
}

func (h *handlers) getStockTransfer(w nethttp.ResponseWriter, r *nethttp.Request) {
	out, err := h.store.GetStockTransfer(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, out)
}

func (h *handlers) createStockTransfer(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req domain.CreateStockTransferRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := req.Validate(); err != nil {
		writeError(w, r, err)
		return
	}
	out, err := h.store.CreateStockTransfer(r.Context(), req)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusCreated, out)
}
