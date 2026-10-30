package http

import (
	"encoding/csv"
	nethttp "net/http"

	"github.com/go-chi/chi/v5"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
)

// ---------------------------------------------------------------------------
// izlenebilirlik: ileri/geri izleme + genealogy (Görev 3)
// ---------------------------------------------------------------------------

func (h *handlers) traceForward(w nethttp.ResponseWriter, r *nethttp.Request) {
	out, err := h.store.TraceForward(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, out)
}

func (h *handlers) traceBackward(w nethttp.ResponseWriter, r *nethttp.Request) {
	out, err := h.store.TraceBackward(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, out)
}

func (h *handlers) genealogy(w nethttp.ResponseWriter, r *nethttp.Request) {
	out, err := h.store.Genealogy(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, out)
}

// ---------------------------------------------------------------------------
// geri çağırma (Görev 3)
// ---------------------------------------------------------------------------

func (h *handlers) listRecalls(w nethttp.ResponseWriter, r *nethttp.Request) {
	p, err := pageFrom(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	items, err := h.store.ListRecalls(r.Context(), p)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]any{"items": items})
}

func (h *handlers) createRecall(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req domain.CreateRecallRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := req.Validate(); err != nil {
		writeError(w, r, err)
		return
	}
	out, err := h.store.CreateRecall(r.Context(), req)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusCreated, out)
}

func (h *handlers) getRecall(w nethttp.ResponseWriter, r *nethttp.Request) {
	out, err := h.store.GetRecall(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, out)
}

func (h *handlers) patchRecallCustomer(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req domain.PatchRecallCustomerRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := req.Validate(); err != nil {
		writeError(w, r, err)
		return
	}
	out, err := h.store.PatchRecallCustomer(r.Context(), chi.URLParam(r, "id"), chi.URLParam(r, "recall_customer_id"), req)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, out)
}

func (h *handlers) closeRecall(w nethttp.ResponseWriter, r *nethttp.Request) {
	out, err := h.store.CloseRecall(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, out)
}

func (h *handlers) reopenRecall(w nethttp.ResponseWriter, r *nethttp.Request) {
	out, err := h.store.ReopenRecall(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, out)
}

// recallContactList: format=csv ise market bazlı iletişim listesini CSV olarak indirir (jüri demo
// için); aksi halde aynı veriyi JSON olarak döner.
func (h *handlers) recallContactList(w nethttp.ResponseWriter, r *nethttp.Request) {
	rc, err := h.store.GetRecall(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	if r.URL.Query().Get("format") != "csv" {
		writeJSON(w, nethttp.StatusOK, map[string]any{"items": rc.Customers})
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+rc.RecallNo+`-iletisim.csv"`)
	wtr := csv.NewWriter(w)
	// kademe: 1 = dogrudan musteri (market), 2 = musterinin KAYITLI ikinci kademe alicisi (dagitan_musteri dolu).
	// Kademe 2 satirinin kalan_risk'i bostur: o miktar dagitan musterinin kalan riskinin icindedir,
	// sutun toplanirsa iki kez sayilmasin. Aliciya giden miktar sevk_miktari sutunundadir.
	_ = wtr.Write([]string{"musteri", "telefon", "mail", "lot", "sevk_miktari", "iade_miktari", "kalan_risk", "durum", "kademe", "dagitan_musteri"})
	for _, c := range rc.Customers {
		phone, email := "", ""
		if c.CustomerPhone != nil {
			phone = *c.CustomerPhone
		}
		if c.CustomerEmail != nil {
			email = *c.CustomerEmail
		}
		_ = wtr.Write([]string{
			c.CustomerName, phone, email, c.LotNo,
			c.QtyShipped.StringFixed(domain.QtyScale), c.QtyReturned.StringFixed(domain.QtyScale),
			c.RemainingRisk.StringFixed(domain.QtyScale), c.ContactStatus, "1", "",
		})
		for _, d := range c.Downstream {
			recipientPhone := ""
			if d.RecipientPhone != nil {
				recipientPhone = *d.RecipientPhone
			}
			_ = wtr.Write([]string{
				d.RecipientName, recipientPhone, "", d.LotNo, d.Qty.StringFixed(domain.QtyScale), "",
				"", "", "2", c.CustomerName,
			})
		}
	}
	wtr.Flush()
}
