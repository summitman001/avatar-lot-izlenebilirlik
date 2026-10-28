package http

import (
	nethttp "net/http"

	"github.com/go-chi/chi/v5"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/repo"
)

// ---------------------------------------------------------------------------
// anomali uyarilari (bkz. docs/anomaly-integration.md)
// ---------------------------------------------------------------------------

func (h *handlers) listAnomalyAlerts(w nethttp.ResponseWriter, r *nethttp.Request) {
	p, err := pageFrom(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	q := r.URL.Query()
	if sv := q.Get("severity"); sv != "" && !domain.AlertSeverities[sv] {
		writeError(w, r, badRequest("severity gecersiz: "+sv))
		return
	}
	if rt := q.Get("ref_type"); rt != "" && !domain.AlertRefTypes[rt] {
		writeError(w, r, badRequest("ref_type gecersiz: "+rt))
		return
	}
	ack, err := boolParam(r, "acknowledged")
	if err != nil {
		writeError(w, r, err)
		return
	}
	items, err := h.store.ListAnomalyAlerts(r.Context(), repo.AnomalyAlertFilter{
		Severity: q.Get("severity"), Acknowledged: ack, RefType: q.Get("ref_type"),
		From: q.Get("from"), To: q.Get("to"), Page: p,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]any{"items": items})
}

func (h *handlers) patchAnomalyAlert(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req domain.PatchAnomalyAlertRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := req.Validate(); err != nil {
		writeError(w, r, err)
		return
	}
	out, err := h.store.PatchAnomalyAlert(r.Context(), chi.URLParam(r, "id"), req)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, out)
}

func (h *handlers) anomalyAlertSummary(w nethttp.ResponseWriter, r *nethttp.Request) {
	out, err := h.store.GetAnomalyAlertSummary(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, out)
}
