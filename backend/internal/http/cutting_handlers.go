package http

import (
	nethttp "net/http"

	"github.com/go-chi/chi/v5"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/repo"
)

// ---------------------------------------------------------------------------
// parcalama (Gorev 2)
// ---------------------------------------------------------------------------

func (h *handlers) listCuttingOrders(w nethttp.ResponseWriter, r *nethttp.Request) {
	p, err := pageFrom(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	q := r.URL.Query()
	if st := q.Get("status"); st != "" && !domain.CuttingOrderStatuses[st] {
		writeError(w, r, badRequest("status gecersiz: "+st))
		return
	}
	items, err := h.store.ListCuttingOrders(r.Context(), repo.CuttingOrderFilter{
		Status: q.Get("status"), InputLotID: q.Get("input_lot_id"), Page: p,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]any{"items": items})
}

func (h *handlers) createCuttingOrder(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req domain.CreateCuttingOrderRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := req.Validate(); err != nil {
		writeError(w, r, err)
		return
	}
	out, err := h.store.CreateCuttingOrder(r.Context(), req)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusCreated, out)
}

func (h *handlers) getCuttingOrder(w nethttp.ResponseWriter, r *nethttp.Request) {
	out, err := h.store.GetCuttingOrder(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, out)
}

func (h *handlers) updateCuttingOutputs(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req domain.UpdateCuttingOutputsRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := req.Validate(); err != nil {
		writeError(w, r, err)
		return
	}
	out, err := h.store.UpdateCuttingOutputs(r.Context(), chi.URLParam(r, "id"), req)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, out)
}

func (h *handlers) closeCuttingOrder(w nethttp.ResponseWriter, r *nethttp.Request) {
	out, err := h.store.CloseCuttingOrder(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, out)
}

func (h *handlers) cuttingOrderMovements(w nethttp.ResponseWriter, r *nethttp.Request) {
	id := chi.URLParam(r, "id")
	if _, err := h.store.GetCuttingOrder(r.Context(), id); err != nil {
		writeError(w, r, err)
		return
	}
	items, err := h.store.ListMovements(r.Context(), repo.MovementFilter{RefType: "cutting_order", RefID: id, Page: repo.Page{Limit: repo.MaxLimit}})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]any{"items": items})
}
