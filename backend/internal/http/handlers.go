package http

import (
	"context"
	nethttp "net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/repo"
)

// validatable: *T Validate() (girdiyi normallestirir ve dogrular).
type validatable[T any] interface {
	*T
	Validate() error
}

// crudOps bir ana veri varligi icin repo cagrilarini baglar. Silme yoktur (is_active / pasif kullanin).
type crudOps[T any] struct {
	list   func(ctx context.Context, r *nethttp.Request) (any, error)
	get    func(ctx context.Context, id string) (any, error)
	create func(ctx context.Context, v *T) (any, error)
	update func(ctx context.Context, id string, v *T) (any, error)
}

// mountCRUD GET /path, POST /path, GET /path/{id}, PUT /path/{id} rotalarini kurar.
// PUT tam guncellemedir (govde varligin tamamini icerir); id URL'den gelir.
func mountCRUD[T any, P validatable[T]](r chi.Router, path string, ops crudOps[T]) {
	r.Get(path, func(w nethttp.ResponseWriter, r *nethttp.Request) {
		items, err := ops.list(r.Context(), r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, nethttp.StatusOK, map[string]any{"items": items})
	})
	r.Post(path, func(w nethttp.ResponseWriter, r *nethttp.Request) {
		var v T
		if err := decodeBody(w, r, &v); err != nil {
			writeError(w, r, err)
			return
		}
		if err := P(&v).Validate(); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := ops.create(r.Context(), &v)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, nethttp.StatusCreated, out)
	})
	r.Get(path+"/{id}", func(w nethttp.ResponseWriter, r *nethttp.Request) {
		out, err := ops.get(r.Context(), chi.URLParam(r, "id"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, nethttp.StatusOK, out)
	})
	r.Put(path+"/{id}", func(w nethttp.ResponseWriter, r *nethttp.Request) {
		var v T
		if err := decodeBody(w, r, &v); err != nil {
			writeError(w, r, err)
			return
		}
		if err := P(&v).Validate(); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := ops.update(r.Context(), chi.URLParam(r, "id"), &v)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, nethttp.StatusOK, out)
	})
}

// ---------------------------------------------------------------------------
// lots
// ---------------------------------------------------------------------------

func (h *handlers) listLots(w nethttp.ResponseWriter, r *nethttp.Request) {
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
	if st := q.Get("status"); st != "" && !domain.LotStatuses[st] {
		writeError(w, r, badRequest("status gecersiz: "+st))
		return
	}
	lots, err := h.store.ListLots(r.Context(), repo.LotFilter{
		ItemID: q.Get("item_id"), Status: q.Get("status"), Q: q.Get("q"), HasMismatch: mismatch, Page: p,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]any{"items": lots})
}

func (h *handlers) getLot(w nethttp.ResponseWriter, r *nethttp.Request) {
	lot, err := h.store.GetLot(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, lot)
}

// createLot alis lotu olusturur: lot + belge satiri + purchase_in hareketi tek transaction'da.
func (h *handlers) createLot(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req domain.PurchaseLotRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := req.Validate(); err != nil {
		writeError(w, r, err)
		return
	}
	lot, err := h.store.CreatePurchaseLot(r.Context(), req)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusCreated, lot)
}

// ---------------------------------------------------------------------------
// stock movements
// ---------------------------------------------------------------------------

func (h *handlers) listMovements(w nethttp.ResponseWriter, r *nethttp.Request) {
	p, err := pageFrom(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	q := r.URL.Query()
	items, err := h.store.ListMovements(r.Context(), repo.MovementFilter{
		LotID: q.Get("lot_id"), RefType: q.Get("ref_type"), RefID: q.Get("ref_id"),
		WarehouseID: q.Get("warehouse_id"), MovementType: q.Get("movement_type"), Page: p,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]any{"items": items})
}

func (h *handlers) reverseMovement(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req domain.ReverseRequest
	if err := decodeOptionalBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := req.Validate(); err != nil {
		writeError(w, r, err)
		return
	}
	m, err := h.store.ReverseMovement(r.Context(), chi.URLParam(r, "id"), req)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusCreated, m)
}

// ---------------------------------------------------------------------------
// dis uretim (Gorev 1)
// ---------------------------------------------------------------------------

func (h *handlers) listProductionOrders(w nethttp.ResponseWriter, r *nethttp.Request) {
	p, err := pageFrom(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	q := r.URL.Query()
	if st := q.Get("status"); st != "" && !domain.ProductionOrderStatuses[st] {
		writeError(w, r, badRequest("status gecersiz: "+st))
		return
	}
	items, err := h.store.ListProductionOrders(r.Context(), repo.ProductionOrderFilter{Status: q.Get("status"), Q: q.Get("q"), Page: p})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]any{"items": items})
}

func (h *handlers) createProductionOrder(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req domain.CreateProductionOrderRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := req.Validate(); err != nil {
		writeError(w, r, err)
		return
	}
	out, err := h.store.CreateProductionOrder(r.Context(), req)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusCreated, out)
}

func (h *handlers) getProductionOrder(w nethttp.ResponseWriter, r *nethttp.Request) {
	out, err := h.store.GetProductionOrder(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, out)
}

func (h *handlers) sendProductionMaterials(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req domain.SendMaterialsRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := req.Validate(); err != nil {
		writeError(w, r, err)
		return
	}
	out, err := h.store.SendMaterials(r.Context(), chi.URLParam(r, "id"), req)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, out)
}

func (h *handlers) receiveProduction(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req domain.ReceiveProductionRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := req.Validate(); err != nil {
		writeError(w, r, err)
		return
	}
	out, err := h.store.ReceiveProduction(r.Context(), chi.URLParam(r, "id"), req)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, out)
}

func (h *handlers) closeProductionOrder(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req domain.CloseProductionOrderRequest
	if err := decodeOptionalBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := req.Validate(); err != nil {
		writeError(w, r, err)
		return
	}
	out, err := h.store.CloseProductionOrder(r.Context(), chi.URLParam(r, "id"), req)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, out)
}

func (h *handlers) productionCostReport(w nethttp.ResponseWriter, r *nethttp.Request) {
	out, err := h.store.GetProductionCostReport(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, out)
}

func (h *handlers) productionMovements(w nethttp.ResponseWriter, r *nethttp.Request) {
	id := chi.URLParam(r, "id")
	if _, err := h.store.GetProductionOrder(r.Context(), id); err != nil {
		writeError(w, r, err)
		return
	}
	items, err := h.store.ListMovements(r.Context(), repo.MovementFilter{RefType: "production_order", RefID: id, Page: repo.Page{Limit: repo.MaxLimit}})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]any{"items": items})
}

// ---------------------------------------------------------------------------
// sync
// ---------------------------------------------------------------------------

func (h *handlers) syncPush(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req domain.SyncPushRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := req.Validate(); err != nil {
		writeError(w, r, err)
		return
	}
	resp, err := h.store.Push(r.Context(), req)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, resp)
}

func (h *handlers) syncPull(w nethttp.ResponseWriter, r *nethttp.Request) {
	q := r.URL.Query()
	var since int64
	if s := q.Get("since_seq"); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n < 0 {
			writeError(w, r, badRequest("since_seq negatif olmayan bir tam sayi olmali"))
			return
		}
		since = n
	}
	limit := 0
	if s := q.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			writeError(w, r, badRequest("limit pozitif bir tam sayi olmali"))
			return
		}
		limit = n
	}
	deviceID := q.Get("device_id")
	if deviceID != "" {
		if _, err := domain.ParseID(deviceID); err != nil {
			writeError(w, r, err)
			return
		}
	}
	resp, err := h.store.Pull(r.Context(), since, limit, deviceID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, resp)
}
