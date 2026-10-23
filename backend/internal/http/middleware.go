package http

import (
	"log/slog"
	nethttp "net/http"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// recoverer panic'i 500 JSON hatasina cevirir.
func recoverer(next nethttp.Handler) nethttp.Handler {
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic", "err", rec, "stack", string(debug.Stack()))
				writeErrorStatus(w, nethttp.StatusInternalServerError, "internal", "beklenmeyen hata")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// requestLogger her istegi slog ile kaydeder.
func requestLogger(next nethttp.Handler) nethttp.Handler {
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		slog.Info("http", "method", r.Method, "path", r.URL.Path, "status", ww.Status(),
			"bytes", ww.BytesWritten(), "ms", time.Since(start).Milliseconds(),
			"request_id", middleware.GetReqID(r.Context()))
	})
}

// cors izinli origin'lere ("*" hepsi) tarayicidan erisim verir. Kimlik dogrulama yok (bkz. HANDOFF.md).
func cors(origins []string) func(nethttp.Handler) nethttp.Handler {
	any := slices.Contains(origins, "*")
	return func(next nethttp.Handler) nethttp.Handler {
		return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && (any || slices.Contains(origins, origin)) {
				if any {
					w.Header().Set("Access-Control-Allow-Origin", "*")
				} else {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Add("Vary", "Origin")
				}
				w.Header().Set("Access-Control-Allow-Methods", strings.Join([]string{"GET", "POST", "PUT", "OPTIONS"}, ", "))
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Request-ID")
				w.Header().Set("Access-Control-Max-Age", "600")
			}
			if r.Method == nethttp.MethodOptions {
				w.WriteHeader(nethttp.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
