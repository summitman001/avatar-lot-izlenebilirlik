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
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	http    *http.Client
}

// New boş baseURL ise nil doner: cagiran taraf nil kontrolu yaparak ML'i tamamen atlayabilir.
func New(baseURL string) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil
	}
	return &Client{baseURL: baseURL, http: &http.Client{Timeout: 3 * time.Second}}
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
