// Package http: chi router, middleware ve JSON handler'lari. Is kurali yok; repo/domain'i cagirir.
package http

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	nethttp "net/http"
	"strconv"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/repo"
)

// Hata govdesi: {"error": {"code": "...", "message": "..."}}
type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// badRequestError: bozuk JSON, gecersiz query parametresi (HTTP 400).
type badRequestError struct{ msg string }

func (e *badRequestError) Error() string { return e.msg }

func badRequest(msg string) error { return &badRequestError{msg} }

func writeJSON(w nethttp.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("json yazilamadi", "err", err)
	}
}

func writeErrorStatus(w nethttp.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, errorBody{errorDetail{Code: code, Message: msg}})
}

// writeError hatayi HTTP status + kod'a cevirir. Bilinmeyen hatalar 500'dur ve ayrintisi istemciye sizdirilmaz.
func writeError(w nethttp.ResponseWriter, r *nethttp.Request, err error) {
	var (
		ve *domain.ValidationError
		br *badRequestError
		ce *domain.ConflictError
	)
	switch {
	case errors.As(err, &br):
		writeErrorStatus(w, nethttp.StatusBadRequest, "bad_request", br.msg)
	case errors.As(err, &ve):
		writeErrorStatus(w, nethttp.StatusUnprocessableEntity, "validation_error", ve.Msg)
	case errors.Is(err, domain.ErrNotFound):
		writeErrorStatus(w, nethttp.StatusNotFound, "not_found", "kayit bulunamadi")
	case errors.As(err, &ce):
		writeErrorStatus(w, nethttp.StatusConflict, "conflict", ce.Msg)
	default:
		slog.Error("istek hatasi", "method", r.Method, "path", r.URL.Path, "err", err)
		writeErrorStatus(w, nethttp.StatusInternalServerError, "internal", "beklenmeyen hata")
	}
}

const maxBodyBytes = 1 << 20 // 1 MiB

// decodeBody JSON govdesini okur: bilinmeyen alan, JSON number ile decimal ve fazla ondalik reddedilir.
func decodeBody(w nethttp.ResponseWriter, r *nethttp.Request, v any) error {
	dec := json.NewDecoder(nethttp.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return badRequest("gecersiz JSON govdesi: " + err.Error())
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return badRequest("govdede tek bir JSON nesnesi olmali")
	}
	return nil
}

// decodeOptionalBody bos govdeye izin verir (reverse gibi tum alanlari opsiyonel isteklerde).
func decodeOptionalBody(w nethttp.ResponseWriter, r *nethttp.Request, v any) error {
	if r.ContentLength == 0 {
		return nil
	}
	return decodeBody(w, r, v)
}

func pageFrom(r *nethttp.Request) (repo.Page, error) {
	var p repo.Page
	q := r.URL.Query()
	for key, dst := range map[string]*int{"limit": &p.Limit, "offset": &p.Offset} {
		if s := q.Get(key); s != "" {
			n, err := strconv.Atoi(s)
			if err != nil || n < 0 {
				return p, badRequest(key + " negatif olmayan bir tam sayi olmali")
			}
			*dst = n
		}
	}
	return p, nil
}

func boolParam(r *nethttp.Request, key string) (*bool, error) {
	s := r.URL.Query().Get(key)
	if s == "" {
		return nil, nil
	}
	b, err := strconv.ParseBool(s)
	if err != nil {
		return nil, badRequest(key + " true veya false olmali")
	}
	return &b, nil
}
