package http

import (
	"io"
	nethttp "net/http"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/repo"
)

// ---------------------------------------------------------------------------
// fatura/irsaliye OCR (bkz. docs/invoice-ocr.md). OCR SADECE oneri uretir; onay (confirm)
// olmadan hicbir lot/stok hareketi/alis fisi olusmaz.
// ---------------------------------------------------------------------------

// invoiceUploadMaxBytes: ML tarafiyla tutarli (ml/app/config.py OCR_MAX_FILE_SIZE_BYTES varsayilani).
const invoiceUploadMaxBytes = 15_000_000

var invoiceAllowedContentTypes = map[string]bool{
	"image/jpeg": true, "image/jpg": true, "image/png": true, "application/pdf": true,
}
var invoiceAllowedExt = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".pdf": true}

func allowedInvoiceFile(contentType, filename string) bool {
	if invoiceAllowedContentTypes[strings.ToLower(strings.TrimSpace(contentType))] {
		return true
	}
	return invoiceAllowedExt[strings.ToLower(filepath.Ext(filename))]
}

func (h *handlers) listInvoiceImports(w nethttp.ResponseWriter, r *nethttp.Request) {
	p, err := pageFrom(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	items, err := h.store.ListInvoiceImports(r.Context(), repo.InvoiceImportFilter{
		Status: r.URL.Query().Get("status"), Page: p,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]any{"items": items})
}

func (h *handlers) getInvoiceImport(w nethttp.ResponseWriter, r *nethttp.Request) {
	out, err := h.store.GetInvoiceImport(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, out)
}

// createInvoiceImport: multipart dosya yukler, ML'e iletir (/extract/invoice), oneriyi
// invoice_import olarak kaydeder. ML kapaliysa 503 + acik mesaj doner, HICBIR KAYIT OLUSMAZ.
func (h *handlers) createInvoiceImport(w nethttp.ResponseWriter, r *nethttp.Request) {
	r.Body = nethttp.MaxBytesReader(w, r.Body, invoiceUploadMaxBytes+1<<20) // multipart govde ek yuku icin pay
	if err := r.ParseMultipartForm(invoiceUploadMaxBytes); err != nil {
		writeError(w, r, badRequest("multipart govde okunamadi veya dosya cok buyuk: "+err.Error()))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, r, badRequest("dosya (file alani, multipart/form-data) gerekli: "+err.Error()))
		return
	}
	defer file.Close()

	content, err := io.ReadAll(io.LimitReader(file, invoiceUploadMaxBytes+1))
	if err != nil {
		writeError(w, r, err)
		return
	}
	if len(content) > invoiceUploadMaxBytes {
		writeError(w, r, badRequest("dosya boyutu siniri asildi"))
		return
	}
	contentType := header.Header.Get("Content-Type")
	if !allowedInvoiceFile(contentType, header.Filename) {
		writeError(w, r, badRequest("desteklenmeyen dosya tipi (jpg/png/pdf bekleniyor): "+contentType))
		return
	}

	out, err := h.store.CreateInvoiceImport(r.Context(), header.Filename, contentType, content)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusCreated, out)
}

func (h *handlers) getInvoiceImportFile(w nethttp.ResponseWriter, r *nethttp.Request) {
	contentType, path, err := h.store.InvoiceImportFile(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	nethttp.ServeFile(w, r, path)
}

// confirmInvoiceImport: govde OCR ciktisi DEGIL, KULLANICININ DUZELTTIGI veridir (ayni sekil:
// CreatePurchaseReceiptRequest). Mevcut alis fisi olusturma yolunu aynen kullanir.
func (h *handlers) confirmInvoiceImport(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req domain.CreatePurchaseReceiptRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := req.Validate(); err != nil {
		writeError(w, r, err)
		return
	}
	out, err := h.store.ConfirmInvoiceImport(r.Context(), chi.URLParam(r, "id"), req)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, out)
}

func (h *handlers) discardInvoiceImport(w nethttp.ResponseWriter, r *nethttp.Request) {
	out, err := h.store.DiscardInvoiceImport(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, out)
}
