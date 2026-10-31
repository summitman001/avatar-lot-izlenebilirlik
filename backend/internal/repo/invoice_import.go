package repo

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
)

// Package repo (invoice_import.go): fatura/irsaliye OCR akisi (bkz. docs/invoice-ocr.md).
//
// KESIN KURAL: OCR SADECE oneri uretir ("dogru" diye sunulmaz). Onay (Confirm) olmadan hicbir
// lot/stok hareketi/alis fisi olusmaz; Confirm, mevcut createPurchaseReceiptInTx'i (repo/purchase.go)
// KULLANICININ DUZELTTIGI veriyle cagirir -- ikinci bir alis fisi olusturma yolu YOKTUR.

// ocrLineNamesProbe ocr_json'dan satir urun adlarini okumak icin minimal bir ayristirma (ml/app/main.py
// InvoiceExtractionResponse ile ayni sekil, ama SADECE ihtiyac duyulan alanlar -- fuzzy eslestirme
// icin, bkz. fuzzy.go).
type ocrLineNamesProbe struct {
	SupplierName struct {
		Value *string `json:"value"`
	} `json:"supplier_name"`
	Lines []struct {
		ProductName struct {
			Value *string `json:"value"`
		} `json:"product_name"`
	} `json:"lines"`
}

func parseOCRNames(raw []byte) (supplierName string, lineNames []string) {
	var probe ocrLineNamesProbe
	if err := json.Unmarshal(raw, &probe); err != nil {
		return "", nil
	}
	if probe.SupplierName.Value != nil {
		supplierName = *probe.SupplierName.Value
	}
	lineNames = make([]string, len(probe.Lines))
	for i, l := range probe.Lines {
		if l.ProductName.Value != nil {
			lineNames[i] = *l.ProductName.Value
		}
	}
	return supplierName, lineNames
}

const invoiceImportListSelect = `
SELECT ii.id, ii.file_name, ii.uploaded_at, ii.status, ii.overall_confidence,
       ii.purchase_receipt_id, pr.doc_no
FROM invoice_import ii
LEFT JOIN purchase_receipt pr ON pr.id = ii.purchase_receipt_id`

type InvoiceImportFilter struct {
	Status string
	Page   Page
}

func (s *Store) ListInvoiceImports(ctx context.Context, f InvoiceImportFilter) ([]domain.InvoiceImportListItem, error) {
	var b query
	if f.Status != "" {
		b.add(`ii.status = ?`, f.Status)
	}
	limit, offset := f.Page.normalized()
	rows, err := s.db.QueryContext(ctx,
		invoiceImportListSelect+b.sql()+` ORDER BY ii.uploaded_at DESC LIMIT ? OFFSET ?`,
		append(b.args, limit, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.InvoiceImportListItem{}
	for rows.Next() {
		var it domain.InvoiceImportListItem
		var receiptID, docNo sql.NullString
		if err := rows.Scan(&it.ID, &it.FileName, &it.UploadedAt, &it.Status, &it.OverallConfidence,
			&receiptID, &docNo); err != nil {
			return nil, err
		}
		if receiptID.Valid {
			it.PurchaseReceiptID = &receiptID.String
		}
		if docNo.Valid {
			it.PurchaseReceiptDocNo = &docNo.String
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// GetInvoiceImport: OCR onerisi (ham) + TAZE hesaplanan tedarikci/urun eslestirme adaylari (bkz.
// fuzzy.go -- DB'de saklanmaz, her istekte yeniden hesaplanir, diger "taze hesaplanan onizleme"
// desenleriyle tutarli, ornegin cutting-orders allocation).
func (s *Store) GetInvoiceImport(ctx context.Context, id string) (*domain.InvoiceImportDetail, error) {
	var d domain.InvoiceImportDetail
	var ocrRaw []byte
	var receiptID sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, file_name, content_type, uploaded_at, status, ocr_json, overall_confidence,
		        purchase_receipt_id, confirmed_at, discarded_at, created_at
		 FROM invoice_import WHERE id = ?`, id).
		Scan(&d.ID, &d.FileName, &d.ContentType, &d.UploadedAt, &d.Status, &ocrRaw, &d.OverallConfidence,
			&receiptID, &d.ConfirmedAt, &d.DiscardedAt, &d.CreatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	if receiptID.Valid {
		d.PurchaseReceiptID = &receiptID.String
	}
	d.OCR = json.RawMessage(ocrRaw)

	supplierName, lineNames := parseOCRNames(ocrRaw)
	supplierCands, err := s.matchSupplierCandidates(ctx, supplierName)
	if err != nil {
		return nil, err
	}
	d.SupplierCandidates = supplierCands
	lineCands := make([][]domain.MatchCandidate, len(lineNames))
	for i, name := range lineNames {
		c, err := s.matchItemCandidates(ctx, name)
		if err != nil {
			return nil, err
		}
		lineCands[i] = c
	}
	d.LineItemCandidates = lineCands
	return &d, nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// CreateInvoiceImport: dosyayi diske yazar, ML'e (/extract/invoice) iletir, sonucu (ham, "dogru"
// diye DEGIL ONERI olarak) invoice_import'a kaydeder (status=extracted). ML KAPALIYSA/HATA
// VERIRSE: diger ML entegrasyonlarindan (tahmin/anomali) FARKLI olarak en iyi gayret YOKTUR --
// OCR olmadan oneri uretilemez, 503 doner, HICBIR KAYIT OLUSMAZ (dosya da yazilmaz).
func (s *Store) CreateInvoiceImport(ctx context.Context, fileName, contentType string, content []byte) (*domain.InvoiceImportDetail, error) {
	if s.ml == nil {
		return nil, domain.Unavailable("OCR servisi kapali, manuel giris yapabilirsiniz")
	}
	ocrRaw, overall, err := s.ml.ExtractInvoice(ctx, fileName, contentType, content)
	if err != nil {
		return nil, domain.Unavailable("OCR servisi kapali, manuel giris yapabilirsiniz")
	}

	id := domain.NewID()
	storedName := id + filepath.Ext(fileName)
	if err := os.MkdirAll(s.uploadsDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(s.uploadsDir, storedName), content, 0o644); err != nil {
		return nil, err
	}

	err = s.InTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO invoice_import (id, file_name, file_hash, content_type, stored_path, ocr_json, overall_confidence)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id, fileName, sha256Hex(content), contentType, storedName, string(ocrRaw), overall)
		return mapErr(err)
	})
	if err != nil {
		_ = os.Remove(filepath.Join(s.uploadsDir, storedName))
		return nil, err
	}
	return s.GetInvoiceImport(ctx, id)
}

// InvoiceImportFile GET /invoice-imports/{id}/file icin saklanan dosyanin yolunu + icerik tipini doner.
func (s *Store) InvoiceImportFile(ctx context.Context, id string) (contentType, absPath string, err error) {
	var storedPath string
	err = s.db.QueryRowContext(ctx, `SELECT content_type, stored_path FROM invoice_import WHERE id = ?`, id).
		Scan(&contentType, &storedPath)
	if err != nil {
		return "", "", mapErr(err)
	}
	return contentType, filepath.Join(s.uploadsDir, storedPath), nil
}

// ConfirmInvoiceImport: govde OCR CIKTISI DEGIL, KULLANICININ DUZELTTIGI veridir (ayni sekil:
// domain.CreatePurchaseReceiptRequest). TEK transaction'da: durum kontrolu (FOR UPDATE, zaten
// confirmed/discarded ise 409) + createPurchaseReceiptInTx (repo/purchase.go ile AYNI fonksiyon,
// bkz. docs/invoice-ocr.md) + invoice_import.status='confirmed'. Boylece onay olmadan hicbir
// kayit olusmadigi VE ayni kaydin iki kez onaylanamayacagi tek bir transaction'da garanti edilir.
func (s *Store) ConfirmInvoiceImport(ctx context.Context, id string, req domain.CreatePurchaseReceiptRequest) (*domain.InvoiceImportDetail, error) {
	var receiptID string
	var lotIDs []string
	var lines []domain.PurchaseReceiptLineRequest
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		var status string
		err := tx.QueryRowContext(ctx, `SELECT status FROM invoice_import WHERE id = ? FOR UPDATE`, id).Scan(&status)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrNotFound
		} else if err != nil {
			return err
		}
		if status != "extracted" {
			return domain.Conflict("fatura kaydi zaten %s durumunda", status)
		}
		receiptID, lotIDs, err = createPurchaseReceiptInTx(ctx, tx, req)
		if err != nil {
			return err
		}
		lines = req.Lines
		_, err = tx.ExecContext(ctx,
			`UPDATE invoice_import SET status = 'confirmed', purchase_receipt_id = ?, confirmed_at = CURRENT_TIMESTAMP(3) WHERE id = ?`,
			receiptID, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	for i, line := range lines {
		s.detectPurchaseCostAnomaly(ctx, receiptID, line.ItemID, lotIDs[i], line.UnitPrice.Decimal)
	}
	return s.GetInvoiceImport(ctx, id)
}

// DeleteInvoiceImport: onaylanmamis (extracted/discarded) kaydi ve dosyasini siler; bunlardan hicbir
// lot/hareket/fis olusmamistir. Onaylanmis kayit alis fisinin kaynak belgesidir, izlenebilirlik
// kaydi oldugu icin silinmez (409).
func (s *Store) DeleteInvoiceImport(ctx context.Context, id string) error {
	var storedPath string
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		var status string
		err := tx.QueryRowContext(ctx, `SELECT status, stored_path FROM invoice_import WHERE id = ? FOR UPDATE`, id).
			Scan(&status, &storedPath)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrNotFound
		} else if err != nil {
			return err
		}
		if status == "confirmed" {
			return domain.Conflict("onaylanmis belge silinemez: alis fisinin kaynagi olarak saklanir")
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM invoice_import WHERE id = ?`, id)
		return err
	})
	if err != nil {
		return err
	}
	// Kayit silindi; dosya kalirsa yalnizca disk yer kaplar, istek basarisiz sayilmaz.
	_ = os.Remove(filepath.Join(s.uploadsDir, storedPath))
	return nil
}

// DiscardInvoiceImport: SADECE 'extracted' durumundaki bir kaydi 'discarded' yapar (zaten
// confirmed/discarded ise 409) -- hicbir lot/hareket/fis etkilenmez, SADECE bu kaydin durumu degisir.
func (s *Store) DiscardInvoiceImport(ctx context.Context, id string) (*domain.InvoiceImportDetail, error) {
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE invoice_import SET status = 'discarded', discarded_at = CURRENT_TIMESTAMP(3) WHERE id = ? AND status = 'extracted'`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			return nil
		}
		var status string
		serr := tx.QueryRowContext(ctx, `SELECT status FROM invoice_import WHERE id = ?`, id).Scan(&status)
		if errors.Is(serr, sql.ErrNoRows) {
			return domain.ErrNotFound
		} else if serr != nil {
			return serr
		}
		return domain.Conflict("fatura kaydi zaten %s durumunda", status)
	})
	if err != nil {
		return nil, err
	}
	return s.GetInvoiceImport(ctx, id)
}
