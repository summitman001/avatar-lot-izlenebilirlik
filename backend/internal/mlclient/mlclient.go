// Package mlclient: ML tahmin servisine (ml/, FastAPI) ince bir HTTP istemcisi.
//
// Lot izlenebilirligi ve geri cagirma ML'e DAYANMAZ; ML sadece tahmindir. Servis kapaliysa veya
// zaman asimina ugrarsa cagiran taraf (repo/production.go) tahmini atlar, islemi BOZMAZ.
package mlclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	http    *http.Client
	ocrHTTP *http.Client // goruntu OCR'i (tesseract) tahmin/anomali cagrilarindan daha uzun surer
}

// New boş baseURL ise nil doner: cagiran taraf nil kontrolu yaparak ML'i tamamen atlayabilir.
func New(baseURL string) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil
	}
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 3 * time.Second},
		ocrHTTP: &http.Client{Timeout: 30 * time.Second},
	}
}

// ProductionPredictionInput ml/app/main.py ProductionRequest ile ayni alanlar.
type ProductionPredictionInput struct {
	EtKg         string  `json:"et_kg"`
	SupplierID   *string `json:"supplier_id,omitempty"`
	ProducerID   *string `json:"producer_id,omitempty"`
	UnitWeightKg string  `json:"unit_weight_kg"`
}

// ProductionPrediction ml/app/main.py ProductionResponse ile ayni alanlar (gerekenler).
type ProductionPrediction struct {
	ModelName             string `json:"model_name"`
	ModelVersion          string `json:"model_version"`
	Baseline              bool   `json:"baseline"`
	SupplierUsed          string `json:"supplier_used"`
	Fallback              bool   `json:"fallback"`
	PredictedYieldRatio   string `json:"predicted_yield_ratio"`
	PredictedOutputKg     string `json:"predicted_output_kg"`
	PredictedDeliveredQty string `json:"predicted_delivered_qty"`
}

// PredictProduction POST /predict/production cagirir. ML'in kendisi maliyet modeli DEGILDIR: yalnizca
// fiziksel verim (adet/kg) tahmin eder; HTTP yanitinda EKP alani YOKTUR. EKP tahmini (hibrit yaklasim:
// bu fiziksel tahmin + kesin malzeme maliyeti) cagiran tarafta (repo/production.go) hesaplanir.
func (c *Client) PredictProduction(ctx context.Context, in ProductionPredictionInput) (*ProductionPrediction, error) {
	var out ProductionPrediction
	if err := c.post(ctx, "/predict/production", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CuttingPredictionInput ml/app/main.py CuttingRequest ile ayni alanlar.
type CuttingPredictionInput struct {
	CarcassKg string  `json:"carcass_kg"`
	Breed     *string `json:"breed,omitempty"`
	Sex       *string `json:"sex,omitempty"`
}

// CuttingPredictionPart ml/app/main.py CuttingPartOut ile ayni alanlar.
type CuttingPredictionPart struct {
	Part  string `json:"part"`
	Kg    string `json:"kg"`
	Ratio string `json:"ratio"`
}

// CuttingPrediction ml/app/main.py CuttingResponse ile ayni alanlar.
type CuttingPrediction struct {
	ModelName         string                  `json:"model_name"`
	ModelVersion      string                  `json:"model_version"`
	Baseline          bool                    `json:"baseline"`
	BreedUsed         string                  `json:"breed_used"`
	Fallback          bool                    `json:"fallback"`
	CarcassKg         string                  `json:"carcass_kg"`
	SellableKg        string                  `json:"sellable_kg"`
	ExpectedFireKg    string                  `json:"expected_fire_kg"`
	ExpectedFireRatio string                  `json:"expected_fire_ratio"`
	Parts             []CuttingPredictionPart `json:"parts"`
}

// PredictCutting POST /predict/cutting cagirir: giris karkas agirligindan (ve varsa irk/cinsiyetten)
// parca basina beklenen agirlik/oran tahmini doner. En iyi gayrettir (bkz. repo/cutting.go).
func (c *Client) PredictCutting(ctx context.Context, in CuttingPredictionInput) (*CuttingPrediction, error) {
	var out CuttingPrediction
	if err := c.post(ctx, "/predict/cutting", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DetectAnomalyInput ml/app/anomaly.py AnomalyRequest ile ayni alanlar. Tum alanlar opsiyoneldir;
// hangileri doluysa o kural calisir (bkz. docs/anomaly-integration.md). Decimal-guvenli: hepsi string.
type DetectAnomalyInput struct {
	ReferenceID string `json:"reference_id,omitempty"`

	// VERIM_DUSUK
	ActualYieldRatio *string `json:"actual_yield_ratio,omitempty"`
	EtKg             *string `json:"et_kg,omitempty"`
	SupplierID       *string `json:"supplier_id,omitempty"`
	ProducerID       *string `json:"producer_id,omitempty"`

	// FIRE_YUKSEK
	ActualFireKg *string `json:"actual_fire_kg,omitempty"`
	CarcassKg    *string `json:"carcass_kg,omitempty"`
	Breed        *string `json:"breed,omitempty"`
	Sex          *string `json:"sex,omitempty"`

	// MALIYET_ARTISI
	CurrentUnitCost *string  `json:"current_unit_cost,omitempty"`
	RecentUnitCosts []string `json:"recent_unit_costs,omitempty"`

	// EKP_SAPMA
	PredictedEkpUnit *string `json:"predicted_ekp_unit,omitempty"`
	ActualEkpUnit    *string `json:"actual_ekp_unit,omitempty"`

	// KUTLE_DENGESI
	InputWeightKg   *string  `json:"input_weight_kg,omitempty"`
	OutputWeightsKg []string `json:"output_weights_kg,omitempty"`
}

// AnomalyFinding ml/app/anomaly.py Finding ile ayni alanlar. Evidence yalnizca string degerler icerir
// (Decimal-guvenli), Go tarafi bunu dogrudan anomaly_alert.evidence_json'a yazabilir.
type AnomalyFinding struct {
	Code     string            `json:"code"`
	Severity string            `json:"severity"`
	Message  string            `json:"message"`
	Evidence map[string]string `json:"evidence"`
}

// DetectAnomalyOutput ml/app/main.py AnomalyResponse ile ayni alanlar.
type DetectAnomalyOutput struct {
	ReferenceID string           `json:"reference_id"`
	Findings    []AnomalyFinding `json:"findings"`
}

// DetectAnomaly POST /detect/anomaly cagirir. En iyi gayrettir: cagiran taraf (repo/anomaly.go) ML
// erisilemezse/hata verirse anomali kontrolunu atlar, asil islemi BOZMAZ.
func (c *Client) DetectAnomaly(ctx context.Context, in DetectAnomalyInput) (*DetectAnomalyOutput, error) {
	var out DetectAnomalyOutput
	if err := c.post(ctx, "/detect/anomaly", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ExtractInvoice POST /extract/invoice cagirir (multipart dosya yuklemesi). ml/app/main.py
// InvoiceExtractionResponse'u OLDUGU GIBI (json.RawMessage) doner -- Go alan alan kopyalamaz,
// invoice_import.ocr_json'a aynen yazilir ve web'e aynen gecirilir (bkz. docs/invoice-ocr.md).
// overallConfidence yaniti ayrica Decimal(5,4) kolonu icin ayristirilir. EN IYI GAYRET DEGILDIR:
// cagiran taraf (repo/invoice_import.go) hata donerse OCR servisi kapali/basarisiz kabul eder,
// kayit OLUSTURMAZ (bkz. docs/invoice-ocr.md, "ML kapaliysa").
func (c *Client) ExtractInvoice(ctx context.Context, filename, contentType string, content []byte) (raw json.RawMessage, overallConfidence float64, err error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, 0, err
	}
	if _, err := part.Write(content); err != nil {
		return nil, 0, err
	}
	if err := writer.Close(); err != nil {
		return nil, 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/extract/invoice", body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	_ = contentType // ML icerik tipini dosya adi/magic byte'tan kendi cikarir (bkz. ml/app/ocr.py)

	res, err := c.ocrHTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	respRaw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, 0, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("ml servisi %d dondu: %s", res.StatusCode, string(respRaw))
	}
	var probe struct {
		OverallConfidence float64 `json:"overall_confidence"`
	}
	if err := json.Unmarshal(respRaw, &probe); err != nil {
		return nil, 0, err
	}
	return json.RawMessage(respRaw), probe.OverallConfidence, nil
}

func (c *Client) post(ctx context.Context, path string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<16))
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("ml servisi %d dondu: %s", res.StatusCode, string(raw))
	}
	return json.Unmarshal(raw, out)
}
