package http

import (
	"context"
	"database/sql"
	nethttp "net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/api"
	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/repo"
)

type Deps struct {
	DB          *sql.DB
	Store       *repo.Store
	CORSOrigins []string
}

// NewRouter tum rotalari kurar. /api/v1 altindaki her sey; /health ve /openapi.yaml kok takma adlaridir
// (docker healthcheck icin).
func NewRouter(d Deps) nethttp.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, recoverer, requestLogger, cors(d.CORSOrigins))

	r.NotFound(func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		writeErrorStatus(w, nethttp.StatusNotFound, "not_found", "rota bulunamadi")
	})
	r.MethodNotAllowed(func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		writeErrorStatus(w, nethttp.StatusMethodNotAllowed, "method_not_allowed", "metot desteklenmiyor")
	})

	h := &handlers{store: d.Store, db: d.DB}
	r.Get("/health", h.health)
	r.Get("/openapi.yaml", openapi)

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/health", h.health)
		r.Get("/openapi.yaml", openapi)

		mountCRUD(r, "/items", crudOps[domain.Item]{
			list: func(ctx context.Context, r *nethttp.Request) (any, error) {
				p, err := pageFrom(r)
				if err != nil {
					return nil, err
				}
				active, err := boolParam(r, "is_active")
				if err != nil {
					return nil, err
				}
				q := r.URL.Query()
				return d.Store.ListItems(ctx, repo.ItemFilter{Q: q.Get("q"), ItemType: q.Get("item_type"), Active: active, Page: p})
			},
			get:    func(ctx context.Context, id string) (any, error) { return d.Store.GetItem(ctx, id) },
			create: func(ctx context.Context, v *domain.Item) (any, error) { return d.Store.CreateItem(ctx, *v) },
			update: func(ctx context.Context, id string, v *domain.Item) (any, error) {
				return d.Store.UpdateItem(ctx, id, *v)
			},
		})
		mountCRUD(r, "/suppliers", crudOps[domain.Supplier]{
			list: func(ctx context.Context, r *nethttp.Request) (any, error) {
				p, err := pageFrom(r)
				if err != nil {
					return nil, err
				}
				q := r.URL.Query()
				return d.Store.ListSuppliers(ctx, repo.SupplierFilter{Q: q.Get("q"), SupplierType: q.Get("supplier_type"), Page: p})
			},
			get:    func(ctx context.Context, id string) (any, error) { return d.Store.GetSupplier(ctx, id) },
			create: func(ctx context.Context, v *domain.Supplier) (any, error) { return d.Store.CreateSupplier(ctx, *v) },
			update: func(ctx context.Context, id string, v *domain.Supplier) (any, error) {
				return d.Store.UpdateSupplier(ctx, id, *v)
			},
		})
		mountCRUD(r, "/customers", crudOps[domain.Customer]{
			list: func(ctx context.Context, r *nethttp.Request) (any, error) {
				p, err := pageFrom(r)
				if err != nil {
					return nil, err
				}
				q := r.URL.Query()
				return d.Store.ListCustomers(ctx, repo.CustomerFilter{Q: q.Get("q"), CustomerType: q.Get("customer_type"), Page: p})
			},
			get:    func(ctx context.Context, id string) (any, error) { return d.Store.GetCustomer(ctx, id) },
			create: func(ctx context.Context, v *domain.Customer) (any, error) { return d.Store.CreateCustomer(ctx, *v) },
			update: func(ctx context.Context, id string, v *domain.Customer) (any, error) {
				return d.Store.UpdateCustomer(ctx, id, *v)
			},
		})
		mountCRUD(r, "/warehouses", crudOps[domain.Warehouse]{
			list: func(ctx context.Context, r *nethttp.Request) (any, error) {
				p, err := pageFrom(r)
				if err != nil {
					return nil, err
				}
				return d.Store.ListWarehouses(ctx, repo.WarehouseFilter{WarehouseType: r.URL.Query().Get("warehouse_type"), Page: p})
			},
			get:    func(ctx context.Context, id string) (any, error) { return d.Store.GetWarehouse(ctx, id) },
			create: func(ctx context.Context, v *domain.Warehouse) (any, error) { return d.Store.CreateWarehouse(ctx, *v) },
			update: func(ctx context.Context, id string, v *domain.Warehouse) (any, error) {
				return d.Store.UpdateWarehouse(ctx, id, *v)
			},
		})

		mountCRUD(r, "/cutting-recipes", crudOps[domain.CuttingRecipe]{
			list: func(ctx context.Context, r *nethttp.Request) (any, error) {
				p, err := pageFrom(r)
				if err != nil {
					return nil, err
				}
				return d.Store.ListCuttingRecipes(ctx, p)
			},
			get: func(ctx context.Context, id string) (any, error) { return d.Store.GetCuttingRecipe(ctx, id) },
			create: func(ctx context.Context, v *domain.CuttingRecipe) (any, error) {
				return d.Store.CreateCuttingRecipe(ctx, *v)
			},
			update: func(ctx context.Context, id string, v *domain.CuttingRecipe) (any, error) {
				return d.Store.UpdateCuttingRecipe(ctx, id, *v)
			},
		})

		r.Get("/cutting-orders", h.listCuttingOrders)
		r.Post("/cutting-orders", h.createCuttingOrder)
		r.Get("/cutting-orders/{id}", h.getCuttingOrder)
		r.Put("/cutting-orders/{id}/outputs", h.updateCuttingOutputs)
		r.Post("/cutting-orders/{id}/close", h.closeCuttingOrder)
		r.Get("/cutting-orders/{id}/movements", h.cuttingOrderMovements)

		r.Get("/production-orders", h.listProductionOrders)
		r.Post("/production-orders", h.createProductionOrder)
		r.Get("/production-orders/{id}", h.getProductionOrder)
		r.Post("/production-orders/{id}/send-materials", h.sendProductionMaterials)
		r.Post("/production-orders/{id}/receive", h.receiveProduction)
		r.Post("/production-orders/{id}/close", h.closeProductionOrder)
		r.Get("/production-orders/{id}/cost-report", h.productionCostReport)
		r.Get("/production-orders/{id}/movements", h.productionMovements)

		r.Get("/lots", h.listLots)
		r.Post("/lots", h.createLot)
		r.Get("/lots/{id}", h.getLot)
		r.Post("/lots/{id}/block", h.blockLot)
		r.Post("/lots/{id}/unblock", h.unblockLot)
		r.Get("/lots/{id}/trace-forward", h.traceForward)
		r.Get("/lots/{id}/trace-backward", h.traceBackward)
		r.Get("/lots/{id}/genealogy", h.genealogy)

		r.Get("/recalls", h.listRecalls)
		r.Post("/recalls", h.createRecall)
		r.Get("/recalls/{id}", h.getRecall)
		r.Patch("/recalls/{id}/customers/{recall_customer_id}", h.patchRecallCustomer)
		r.Post("/recalls/{id}/close", h.closeRecall)
		r.Post("/recalls/{id}/reopen", h.reopenRecall)
		r.Get("/recalls/{id}/contact-list", h.recallContactList)

		r.Get("/stock-movements", h.listMovements)
		r.Post("/stock-movements/{id}/reverse", h.reverseMovement)

		r.Get("/sales-orders", h.listSalesOrders)
		r.Post("/sales-orders", h.createSalesOrder)
		r.Get("/sales-orders/{id}", h.getSalesOrder)
		r.Post("/customer-returns", h.createCustomerReturn)

		r.Get("/anomaly-alerts", h.listAnomalyAlerts)
		r.Get("/anomaly-alerts/summary", h.anomalyAlertSummary)
		r.Patch("/anomaly-alerts/{id}", h.patchAnomalyAlert)

		r.Post("/sync/push", h.syncPush)
		r.Get("/sync/pull", h.syncPull)
	})
	return r
}

type handlers struct {
	store *repo.Store
	db    *sql.DB
}

func (h *handlers) health(w nethttp.ResponseWriter, r *nethttp.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := h.db.PingContext(ctx); err != nil {
		writeErrorStatus(w, nethttp.StatusServiceUnavailable, "unavailable", "veritabani erisilemiyor")
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]string{"status": "ok", "time": time.Now().UTC().Format(time.RFC3339)})
}

func openapi(w nethttp.ResponseWriter, _ *nethttp.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	_, _ = w.Write(api.OpenAPI)
}
