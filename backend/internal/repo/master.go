package repo

import (
	"context"
	"database/sql"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
)

// Ana veriler: item, supplier, customer, warehouse.
// item, customer ve warehouse degisiklikleri change_log'a da yazilir (sync pull); supplier sync'e acik degil.

// ---------------------------------------------------------------------------
// item
// ---------------------------------------------------------------------------

const itemCols = `id, code, name, item_type, base_uom, unit_weight_kg, default_sale_price, is_active, created_at`

func scanItem(row interface{ Scan(...any) error }) (*domain.Item, error) {
	var it domain.Item
	err := row.Scan(&it.ID, &it.Code, &it.Name, &it.ItemType, &it.BaseUOM,
		&it.UnitWeightKg, &it.DefaultSalePrice, &it.IsActive, &it.CreatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return &it, nil
}

func getItem(ctx context.Context, q DBTX, id string) (*domain.Item, error) {
	return scanItem(q.QueryRowContext(ctx, `SELECT `+itemCols+` FROM item WHERE id = ?`, id))
}

func (s *Store) GetItem(ctx context.Context, id string) (*domain.Item, error) {
	return getItem(ctx, s.db, id)
}

type ItemFilter struct {
	Q        string
	ItemType string
	Active   *bool
	Page     Page
}

func (s *Store) ListItems(ctx context.Context, f ItemFilter) ([]domain.Item, error) {
	var b query
	if f.Q != "" {
		b.add(`(code LIKE ? OR name LIKE ?)`, like(f.Q), like(f.Q))
	}
	if f.ItemType != "" {
		b.add(`item_type = ?`, f.ItemType)
	}
	if f.Active != nil {
		b.add(`is_active = ?`, *f.Active)
	}
	limit, offset := f.Page.normalized()
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+itemCols+` FROM item`+b.sql()+` ORDER BY code LIMIT ? OFFSET ?`,
		append(b.args, limit, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

func (s *Store) CreateItem(ctx context.Context, in domain.Item) (*domain.Item, error) {
	id, err := resolveID(in.ID)
	if err != nil {
		return nil, err
	}
	var out *domain.Item
	err = s.InTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO item (id, code, name, item_type, base_uom, unit_weight_kg, default_sale_price, is_active)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			id, in.Code, in.Name, in.ItemType, in.BaseUOM, in.UnitWeightKg, in.DefaultSalePrice, in.IsActive); err != nil {
			return mapErr(err)
		}
		if out, err = getItem(ctx, tx, id); err != nil {
			return err
		}
		return appendChange(ctx, tx, "item", id, "upsert", out)
	})
	return out, err
}

func (s *Store) UpdateItem(ctx context.Context, id string, in domain.Item) (*domain.Item, error) {
	var out *domain.Item
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE item SET code=?, name=?, item_type=?, base_uom=?, unit_weight_kg=?, default_sale_price=?, is_active=?
			 WHERE id = ?`,
			in.Code, in.Name, in.ItemType, in.BaseUOM, in.UnitWeightKg, in.DefaultSalePrice, in.IsActive, id)
		if err != nil {
			return mapErr(err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return domain.ErrNotFound
		}
		if out, err = getItem(ctx, tx, id); err != nil {
			return err
		}
		return appendChange(ctx, tx, "item", id, "upsert", out)
	})
	return out, err
}

// ---------------------------------------------------------------------------
// supplier
// ---------------------------------------------------------------------------

const supplierCols = `id, code, name, supplier_type, contact_name, phone, email, address, created_at`

func scanSupplier(row interface{ Scan(...any) error }) (*domain.Supplier, error) {
	var s domain.Supplier
	err := row.Scan(&s.ID, &s.Code, &s.Name, &s.SupplierType, &s.ContactName, &s.Phone, &s.Email, &s.Address, &s.CreatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return &s, nil
}

func getSupplier(ctx context.Context, q DBTX, id string) (*domain.Supplier, error) {
	return scanSupplier(q.QueryRowContext(ctx, `SELECT `+supplierCols+` FROM supplier WHERE id = ?`, id))
}

func (s *Store) GetSupplier(ctx context.Context, id string) (*domain.Supplier, error) {
	return getSupplier(ctx, s.db, id)
}

type SupplierFilter struct {
	Q            string
	SupplierType string
	Page         Page
}

func (s *Store) ListSuppliers(ctx context.Context, f SupplierFilter) ([]domain.Supplier, error) {
	var b query
	if f.Q != "" {
		b.add(`(code LIKE ? OR name LIKE ?)`, like(f.Q), like(f.Q))
	}
	if f.SupplierType != "" {
		b.add(`supplier_type = ?`, f.SupplierType)
	}
	limit, offset := f.Page.normalized()
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+supplierCols+` FROM supplier`+b.sql()+` ORDER BY code LIMIT ? OFFSET ?`,
		append(b.args, limit, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Supplier{}
	for rows.Next() {
		v, err := scanSupplier(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (s *Store) CreateSupplier(ctx context.Context, in domain.Supplier) (*domain.Supplier, error) {
	id, err := resolveID(in.ID)
	if err != nil {
		return nil, err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO supplier (id, code, name, supplier_type, contact_name, phone, email, address)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, in.Code, in.Name, in.SupplierType, in.ContactName, in.Phone, in.Email, in.Address)
	if err != nil {
		return nil, mapErr(err)
	}
	return getSupplier(ctx, s.db, id)
}

func (s *Store) UpdateSupplier(ctx context.Context, id string, in domain.Supplier) (*domain.Supplier, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE supplier SET code=?, name=?, supplier_type=?, contact_name=?, phone=?, email=?, address=? WHERE id = ?`,
		in.Code, in.Name, in.SupplierType, in.ContactName, in.Phone, in.Email, in.Address, id)
	if err != nil {
		return nil, mapErr(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, domain.ErrNotFound
	}
	return getSupplier(ctx, s.db, id)
}

// ---------------------------------------------------------------------------
// customer
// ---------------------------------------------------------------------------

const customerCols = `id, code, name, customer_type, contact_name, phone, email, address, created_at`

func scanCustomer(row interface{ Scan(...any) error }) (*domain.Customer, error) {
	var c domain.Customer
	err := row.Scan(&c.ID, &c.Code, &c.Name, &c.CustomerType, &c.ContactName, &c.Phone, &c.Email, &c.Address, &c.CreatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return &c, nil
}

func getCustomer(ctx context.Context, q DBTX, id string) (*domain.Customer, error) {
	return scanCustomer(q.QueryRowContext(ctx, `SELECT `+customerCols+` FROM customer WHERE id = ?`, id))
}

func (s *Store) GetCustomer(ctx context.Context, id string) (*domain.Customer, error) {
	return getCustomer(ctx, s.db, id)
}

type CustomerFilter struct {
	Q            string
	CustomerType string
	Page         Page
}

func (s *Store) ListCustomers(ctx context.Context, f CustomerFilter) ([]domain.Customer, error) {
	var b query
	if f.Q != "" {
		b.add(`(code LIKE ? OR name LIKE ?)`, like(f.Q), like(f.Q))
	}
	if f.CustomerType != "" {
		b.add(`customer_type = ?`, f.CustomerType)
	}
	limit, offset := f.Page.normalized()
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+customerCols+` FROM customer`+b.sql()+` ORDER BY code LIMIT ? OFFSET ?`,
		append(b.args, limit, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Customer{}
	for rows.Next() {
		v, err := scanCustomer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (s *Store) CreateCustomer(ctx context.Context, in domain.Customer) (*domain.Customer, error) {
	id, err := resolveID(in.ID)
	if err != nil {
		return nil, err
	}
	var out *domain.Customer
	err = s.InTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO customer (id, code, name, customer_type, contact_name, phone, email, address)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			id, in.Code, in.Name, in.CustomerType, in.ContactName, in.Phone, in.Email, in.Address); err != nil {
			return mapErr(err)
		}
		if out, err = getCustomer(ctx, tx, id); err != nil {
			return err
		}
		return appendChange(ctx, tx, "customer", id, "upsert", out)
	})
	return out, err
}

func (s *Store) UpdateCustomer(ctx context.Context, id string, in domain.Customer) (*domain.Customer, error) {
	var out *domain.Customer
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE customer SET code=?, name=?, customer_type=?, contact_name=?, phone=?, email=?, address=? WHERE id = ?`,
			in.Code, in.Name, in.CustomerType, in.ContactName, in.Phone, in.Email, in.Address, id)
		if err != nil {
			return mapErr(err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return domain.ErrNotFound
		}
		if out, err = getCustomer(ctx, tx, id); err != nil {
			return err
		}
		return appendChange(ctx, tx, "customer", id, "upsert", out)
	})
	return out, err
}

// ---------------------------------------------------------------------------
// warehouse
// ---------------------------------------------------------------------------

const warehouseCols = `id, code, name, warehouse_type, created_at`

func scanWarehouse(row interface{ Scan(...any) error }) (*domain.Warehouse, error) {
	var w domain.Warehouse
	if err := row.Scan(&w.ID, &w.Code, &w.Name, &w.WarehouseType, &w.CreatedAt); err != nil {
		return nil, mapErr(err)
	}
	return &w, nil
}

func getWarehouse(ctx context.Context, q DBTX, id string) (*domain.Warehouse, error) {
	return scanWarehouse(q.QueryRowContext(ctx, `SELECT `+warehouseCols+` FROM warehouse WHERE id = ?`, id))
}

func (s *Store) GetWarehouse(ctx context.Context, id string) (*domain.Warehouse, error) {
	return getWarehouse(ctx, s.db, id)
}

type WarehouseFilter struct {
	WarehouseType string
	Page          Page
}

func (s *Store) ListWarehouses(ctx context.Context, f WarehouseFilter) ([]domain.Warehouse, error) {
	var b query
	if f.WarehouseType != "" {
		b.add(`warehouse_type = ?`, f.WarehouseType)
	}
	limit, offset := f.Page.normalized()
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+warehouseCols+` FROM warehouse`+b.sql()+` ORDER BY code LIMIT ? OFFSET ?`,
		append(b.args, limit, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Warehouse{}
	for rows.Next() {
		v, err := scanWarehouse(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (s *Store) CreateWarehouse(ctx context.Context, in domain.Warehouse) (*domain.Warehouse, error) {
	id, err := resolveID(in.ID)
	if err != nil {
		return nil, err
	}
	var out *domain.Warehouse
	err = s.InTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO warehouse (id, code, name, warehouse_type) VALUES (?, ?, ?, ?)`,
			id, in.Code, in.Name, in.WarehouseType); err != nil {
			return mapErr(err)
		}
		if out, err = getWarehouse(ctx, tx, id); err != nil {
			return err
		}
		return appendChange(ctx, tx, "warehouse", id, "upsert", out)
	})
	return out, err
}

func (s *Store) UpdateWarehouse(ctx context.Context, id string, in domain.Warehouse) (*domain.Warehouse, error) {
	var out *domain.Warehouse
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE warehouse SET code=?, name=?, warehouse_type=? WHERE id = ?`,
			in.Code, in.Name, in.WarehouseType, id)
		if err != nil {
			return mapErr(err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return domain.ErrNotFound
		}
		if out, err = getWarehouse(ctx, tx, id); err != nil {
			return err
		}
		return appendChange(ctx, tx, "warehouse", id, "upsert", out)
	})
	return out, err
}
