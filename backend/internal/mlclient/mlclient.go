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
// fiziksel verim (adet/kg) tahmin eder, EKP tahmini doner ONEMSIZ (bkz. docs/assumptions.md).
func (c *Client) PredictProduction(ctx context.Context, in ProductionPredictionInput) (*ProductionPrediction, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/predict/production", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<16))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ml servisi %d dondu: %s", res.StatusCode, string(raw))
	}
	var out ProductionPrediction
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
