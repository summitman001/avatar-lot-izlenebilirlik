package http

import (
	nethttp "net/http"

	"github.com/go-chi/chi/v5"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
)

// ---------------------------------------------------------------------------
// fire, yeniden isleme, ikinci kademe dagitim (bkz. repo/lot_ops.go)
// ---------------------------------------------------------------------------

func (h *handlers) recordWaste(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req domain.WasteRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := req.Validate(); err != nil {
		writeError(w, r, err)
		return
	}
	out, err := h.store.RecordWaste(r.Context(), chi.URLParam(r, "id"), req)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusCreated, out)
}

func (h *handlers) listReprocessOrders(w nethttp.ResponseWriter, r *nethttp.Request) {
	p, err := pageFrom(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	items, err := h.store.ListReprocessOrders(r.Context(), p)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]any{"items": items})
}

func (h *handlers) getReprocessOrder(w nethttp.ResponseWriter, r *nethttp.Request) {
	out, err := h.store.GetReprocessOrder(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, out)
}

func (h *handlers) createReprocessOrder(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req domain.CreateReprocessOrderRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := req.Validate(); err != nil {
		writeError(w, r, err)
		return
	}
	out, err := h.store.CreateReprocessOrder(r.Context(), req)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusCreated, out)
}

func (h *handlers) listDownstream(w nethttp.ResponseWriter, r *nethttp.Request) {
	q := r.URL.Query()
	items, err := h.store.ListDownstream(r.Context(), q.Get("customer_id"), q.Get("lot_id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]any{"items": items})
}

func (h *handlers) createDownstream(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req domain.CreateDownstreamRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := req.Validate(); err != nil {
		writeError(w, r, err)
		return
	}
	out, err := h.store.CreateDownstream(r.Context(), req)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusCreated, out)
}
