package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
)

// İzlenebilirlik (ileri/geri/genealogy) + geri çağırma (Görev 3).
//
// TÜM sorgular lot_link üzerinde MySQL 8 WITH RECURSIVE CTE'leri kullanır (ileri: parent->child,
// geri: child->parent); hiçbir yerde sabit kodlama veya ML çağrısı yoktur -- sonuçlar tamamen
// kesin kayıt ilişkilerinden (lot_link, sales_line, shipment_line, customer_return) hesaplanır.
// Döngü koruması: path sütunu (virgülle ayrılmış lot id listesi) + FIND_IN_SET ile aynı lot
// aynı dalda ikinci kez ziyaret edilmez (bkz. docs/traceability.md).

func strDeref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// ---------------------------------------------------------------------------
// trace-forward (ileri izleme)
// ---------------------------------------------------------------------------

type forwardEdge struct {
	LotID         string
	Depth         int
	Path          string
	ParentLotID   *string
	QtyFromParent *decimal.Decimal
	ProcessType   *string
	ProcessID     *string
	UOM           *string
}

const forwardCTE = `
WITH RECURSIVE fwd AS (
  SELECT l.id AS lot_id, 0 AS depth, CAST(l.id AS CHAR(4000)) AS path,
         CAST(NULL AS CHAR(36)) AS parent_lot_id, CAST(NULL AS DECIMAL(14,3)) AS qty_from_parent,
         CAST(NULL AS CHAR(20)) AS process_type, CAST(NULL AS CHAR(36)) AS process_id,
         CAST(NULL AS CHAR(10)) AS uom
  FROM lot l WHERE l.id = ?
  UNION ALL
  SELECT ll.child_lot_id, fwd.depth + 1, CONCAT(fwd.path, ',', ll.child_lot_id),
         ll.parent_lot_id, ll.qty_from_parent, ll.process_type, ll.process_id, CAST(ll.uom AS CHAR(10))
  FROM lot_link ll
  JOIN fwd ON fwd.lot_id = ll.parent_lot_id
  WHERE NOT FIND_IN_SET(ll.child_lot_id, fwd.path)
)
SELECT lot_id, depth, path, parent_lot_id, qty_from_parent, process_type, process_id, uom FROM fwd`

// fetchForwardEdges: kök lottan başlayıp lot_link.parent->child yönünde TÜM dalları (çok seviyeli,
// çok yollu) döner. Her satır bir KENARDIR (aynı lot, ayrı ebeveynlerden ulaşılmışsa birden fazla
// kez görünebilir); düğüm listesi için çağıran taraf lot_id bazında TEKİLLEŞTİRİR (en küçük depth).
func fetchForwardEdges(ctx context.Context, q DBTX, rootLotID string) ([]forwardEdge, error) {
	rows, err := q.QueryContext(ctx, forwardCTE+` ORDER BY depth, lot_id`, rootLotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []forwardEdge
	for rows.Next() {
		var e forwardEdge
		if err := rows.Scan(&e.LotID, &e.Depth, &e.Path, &e.ParentLotID, &e.QtyFromParent, &e.ProcessType, &e.ProcessID, &e.UOM); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func loadTraceShipments(ctx context.Context, q DBTX, lotID string) ([]domain.TraceShipmentLine, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT sh.shipment_no, sh.customer_id, c.name, shl.qty, sh.shipped_at
		 FROM shipment_line shl JOIN shipment sh ON sh.id = shl.shipment_id JOIN customer c ON c.id = sh.customer_id
		 WHERE shl.lot_id = ? ORDER BY sh.shipped_at`, lotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.TraceShipmentLine{}
	for rows.Next() {
		var l domain.TraceShipmentLine
		if err := rows.Scan(&l.ShipmentNo, &l.CustomerID, &l.CustomerName, &l.Qty, &l.ShippedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func loadTraceSales(ctx context.Context, q DBTX, lotID string) ([]domain.TraceSaleLine, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT so.order_no, so.customer_id, c.name, sl.qty, so.sold_at, sl.recall_violation
		 FROM sales_line sl JOIN sales_order so ON so.id = sl.sales_order_id JOIN customer c ON c.id = so.customer_id
		 WHERE sl.lot_id = ? ORDER BY so.sold_at`, lotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.TraceSaleLine{}
	for rows.Next() {
		var l domain.TraceSaleLine
		if err := rows.Scan(&l.OrderNo, &l.CustomerID, &l.CustomerName, &l.Qty, &l.SoldAt, &l.RecallViolation); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// TraceForward: GET /lots/{id}/trace-forward. Kök dahil, türeyen TÜM mamul/alt lotları (çok
// seviyeli, çok yollu) + her biri için güncel stok/sevkiyat/satış/iade bilgisini döner.
func (s *Store) TraceForward(ctx context.Context, lotID string) (*domain.TraceForwardResponse, error) {
	root, err := getLot(ctx, s.db, lotID, false)
	if err != nil {
		return nil, err
	}
	edges, err := fetchForwardEdges(ctx, s.db, lotID)
	if err != nil {
		return nil, err
	}

	order := []string{}
	primary := map[string]forwardEdge{}
	for _, e := range edges {
		if _, ok := primary[e.LotID]; !ok {
			primary[e.LotID] = e
			order = append(order, e.LotID)
		}
	}

	nodes := make([]domain.TraceForwardNode, 0, len(order))
	remainingByUOM := map[string]decimal.Decimal{}
	custAgg := map[string]*domain.TraceForwardCustomerSummary{}

	for _, id := range order {
		e := primary[id]
		lot, err := getLot(ctx, s.db, id, true)
		if err != nil {
			return nil, err
		}
		node := domain.TraceForwardNode{
			LotID: lot.ID, LotNo: lot.LotNo, ItemID: lot.ItemID, ItemName: lot.ItemName,
			UOM: lot.UOM, Status: lot.Status, Depth: e.Depth, Path: e.Path,
			ParentLotID: e.ParentLotID, ProcessType: e.ProcessType, ProcessID: e.ProcessID,
			TotalProducedQty: lot.InitialQty,
		}
		if e.QtyFromParent != nil {
			q := domain.NewQty(*e.QtyFromParent)
			node.QtyFromParent = &q
			if lot.InitialQty.IsPositive() {
				rs := e.QtyFromParent.DivRound(lot.InitialQty.Decimal, 4).StringFixed(4)
				node.RatioFromParent = &rs
			}
		}
		node.CurrentStock = lot.Balances
		total := decimal.Zero
		for _, b := range lot.Balances {
			total = total.Add(b.QtyOnHand.Decimal)
		}
		node.QtyOnHandTotal = domain.NewQty(total)
		remainingByUOM[lot.UOM] = remainingByUOM[lot.UOM].Add(total)

		shipments, err := loadTraceShipments(ctx, s.db, id)
		if err != nil {
			return nil, err
		}
		node.Shipments = shipments
		sales, err := loadTraceSales(ctx, s.db, id)
		if err != nil {
			return nil, err
		}
		node.Sales = sales
		var returned decimal.Decimal
		if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(qty),0) FROM customer_return WHERE lot_id = ?`, id).Scan(&returned); err != nil {
			return nil, err
		}
		node.ReturnedQty = domain.NewQty(returned)
		nodes = append(nodes, node)

		for _, sl := range sales {
			agg := custAgg[sl.CustomerID]
			if agg == nil {
				agg = &domain.TraceForwardCustomerSummary{CustomerID: sl.CustomerID, CustomerName: sl.CustomerName}
				custAgg[sl.CustomerID] = agg
			}
			agg.QtyShipped = domain.NewQty(agg.QtyShipped.Decimal.Add(sl.Qty.Decimal))
		}

		retRows, err := s.db.QueryContext(ctx, `SELECT customer_id, COALESCE(SUM(qty),0) FROM customer_return WHERE lot_id = ? GROUP BY customer_id`, id)
		if err != nil {
			return nil, err
		}
		type custReturn struct {
			id  string
			qty decimal.Decimal
		}
		var custReturns []custReturn
		for retRows.Next() {
			var cr custReturn
			if err := retRows.Scan(&cr.id, &cr.qty); err != nil {
				retRows.Close()
				return nil, err
			}
			custReturns = append(custReturns, cr)
		}
		if err := retRows.Err(); err != nil {
			retRows.Close()
			return nil, err
		}
		retRows.Close()
		for _, cr := range custReturns {
			agg := custAgg[cr.id]
			if agg == nil {
				var name string
				_ = s.db.QueryRowContext(ctx, `SELECT name FROM customer WHERE id = ?`, cr.id).Scan(&name)
				agg = &domain.TraceForwardCustomerSummary{CustomerID: cr.id, CustomerName: name}
				custAgg[cr.id] = agg
			}
			agg.QtyReturned = domain.NewQty(agg.QtyReturned.Decimal.Add(cr.qty))
		}
	}

	remainSlice := make([]domain.UomQty, 0, len(remainingByUOM))
	uoms := make([]string, 0, len(remainingByUOM))
	for u := range remainingByUOM {
		uoms = append(uoms, u)
	}
	sort.Strings(uoms)
	for _, u := range uoms {
		remainSlice = append(remainSlice, domain.UomQty{UOM: u, Qty: domain.NewQty(remainingByUOM[u])})
	}

	custIDs := make([]string, 0, len(custAgg))
	for id := range custAgg {
		custIDs = append(custIDs, id)
	}
	sort.Slice(custIDs, func(i, j int) bool { return custAgg[custIDs[i]].CustomerName < custAgg[custIDs[j]].CustomerName })
	custSlice := make([]domain.TraceForwardCustomerSummary, 0, len(custIDs))
	for _, id := range custIDs {
		c := *custAgg[id]
		entries, err := loadDownstream(ctx, s.db, id, order)
		if err != nil {
			return nil, err
		}
		c.Downstream = entries
		c.DownstreamKnown, c.DownstreamNote = downstreamStatus(entries, c.QtyShipped.Decimal.Sub(c.QtyReturned.Decimal))
		custSlice = append(custSlice, c)
	}

	return &domain.TraceForwardResponse{
		RootLotID: root.ID, RootLotNo: root.LotNo, Nodes: nodes,
		Summary: domain.TraceForwardSummary{
			AffectedLotCount: len(nodes), RemainingStock: remainSlice, Customers: custSlice,
		},
	}, nil
}

// ---------------------------------------------------------------------------
// trace-backward (geri izleme)
// ---------------------------------------------------------------------------

type backwardEdge struct {
	LotID       string
	Depth       int
	Path        string
	ChildLotID  *string
	QtyUsed     *decimal.Decimal
	ProcessType *string
	ProcessID   *string
	UOM         *string
}

const backwardCTE = `
WITH RECURSIVE bwd AS (
  SELECT l.id AS lot_id, 0 AS depth, CAST(l.id AS CHAR(4000)) AS path,
         CAST(NULL AS CHAR(36)) AS child_lot_id, CAST(NULL AS DECIMAL(14,3)) AS qty_from_parent,
         CAST(NULL AS CHAR(20)) AS process_type, CAST(NULL AS CHAR(36)) AS process_id,
         CAST(NULL AS CHAR(10)) AS uom
  FROM lot l WHERE l.id = ?
  UNION ALL
  SELECT ll.parent_lot_id, bwd.depth + 1, CONCAT(bwd.path, ',', ll.parent_lot_id),
         ll.child_lot_id, ll.qty_from_parent, ll.process_type, ll.process_id, CAST(ll.uom AS CHAR(10))
  FROM lot_link ll
  JOIN bwd ON bwd.lot_id = ll.child_lot_id
  WHERE NOT FIND_IN_SET(ll.parent_lot_id, bwd.path)
)
SELECT lot_id, depth, path, child_lot_id, qty_from_parent, process_type, process_id, uom FROM bwd`

func fetchBackwardEdges(ctx context.Context, q DBTX, rootLotID string) ([]backwardEdge, error) {
	rows, err := q.QueryContext(ctx, backwardCTE+` ORDER BY depth, lot_id`, rootLotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []backwardEdge
	for rows.Next() {
		var e backwardEdge
		if err := rows.Scan(&e.LotID, &e.Depth, &e.Path, &e.ChildLotID, &e.QtyUsed, &e.ProcessType, &e.ProcessID, &e.UOM); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// resolveProcessOrderNo: lot_link.process_id, process_type'a göre hangi tabloya işaret ettiğini
// (production_order | cutting_order | reprocess_order) çözüp o belgenin insan okunur numarasını döner
// (yoksa nil; repack için servis/tablo olmadığından nil kalır).
func resolveProcessOrderNo(ctx context.Context, q DBTX, processType, processID string) *string {
	var table string
	switch processType {
	case "production":
		table = "production_order"
	case "cutting":
		table = "cutting_order"
	case "reprocess":
		table = "reprocess_order"
	default:
		return nil
	}
	var orderNo string
	if err := q.QueryRowContext(ctx, `SELECT order_no FROM `+table+` WHERE id = ?`, processID).Scan(&orderNo); err != nil {
		return nil
	}
	return &orderNo
}

// TraceBackward: GET /lots/{id}/trace-backward. Verilen mamul/ara lottan yukarı, kullanılan TÜM
// hammadde lotlarını (çok seviyeli), tedarikçi iletişim bilgisiyle birlikte döner.
func (s *Store) TraceBackward(ctx context.Context, lotID string) (*domain.TraceBackwardResponse, error) {
	root, err := getLot(ctx, s.db, lotID, false)
	if err != nil {
		return nil, err
	}
	edges, err := fetchBackwardEdges(ctx, s.db, lotID)
	if err != nil {
		return nil, err
	}

	nodes := make([]domain.TraceBackwardNode, 0, len(edges))
	rawLots := map[string]bool{}
	suppliers := map[string]bool{}
	for _, e := range edges {
		if e.Depth == 0 {
			continue // kök kendisi hammadde değil
		}
		lot, err := getLot(ctx, s.db, e.LotID, false)
		if err != nil {
			return nil, err
		}
		n := domain.TraceBackwardNode{
			LotID: lot.ID, LotNo: lot.LotNo, ItemID: lot.ItemID, ItemName: lot.ItemName, UOM: lot.UOM,
			SupplierID: lot.SupplierID, SupplierName: lot.SupplierName,
			ReceivedOrProducedAt: lot.ReceivedOrProducedAt, ExpiryDate: lot.ExpiryDate,
			Depth: e.Depth, Path: e.Path, ChildLotID: e.ChildLotID,
			ProcessType: e.ProcessType, ProcessID: e.ProcessID,
		}
		if e.QtyUsed != nil {
			q := domain.NewQty(*e.QtyUsed)
			n.QtyUsed = &q
		}
		if lot.SupplierID != nil {
			var contactName, phone, email *string
			_ = s.db.QueryRowContext(ctx, `SELECT contact_name, phone, email FROM supplier WHERE id = ?`, *lot.SupplierID).
				Scan(&contactName, &phone, &email)
			n.SupplierContactName, n.SupplierPhone, n.SupplierEmail = contactName, phone, email
			suppliers[*lot.SupplierID] = true
		}
		if e.ProcessType != nil && e.ProcessID != nil {
			n.OrderNo = resolveProcessOrderNo(ctx, s.db, *e.ProcessType, *e.ProcessID)
		}
		rawLots[lot.ID] = true
		nodes = append(nodes, n)
	}

	return &domain.TraceBackwardResponse{
		RootLotID: root.ID, RootLotNo: root.LotNo, Nodes: nodes,
		Summary: domain.TraceBackwardSummary{RawLotCount: len(rawLots), SupplierCount: len(suppliers)},
	}, nil
}

// ---------------------------------------------------------------------------
// genealogy (tek çağrıda yukarı + aşağı, grafik görselleştirmesi için)
// ---------------------------------------------------------------------------

func (s *Store) Genealogy(ctx context.Context, lotID string) (*domain.GenealogyResponse, error) {
	root, err := getLot(ctx, s.db, lotID, false)
	if err != nil {
		return nil, err
	}
	fedges, err := fetchForwardEdges(ctx, s.db, lotID)
	if err != nil {
		return nil, err
	}
	bedges, err := fetchBackwardEdges(ctx, s.db, lotID)
	if err != nil {
		return nil, err
	}

	level := map[string]int{root.ID: 0}
	for _, e := range fedges {
		if e.Depth > 0 {
			if _, ok := level[e.LotID]; !ok {
				level[e.LotID] = e.Depth
			}
		}
	}
	for _, e := range bedges {
		if e.Depth > 0 {
			if _, ok := level[e.LotID]; !ok {
				level[e.LotID] = -e.Depth
			}
		}
	}
	ids := make([]string, 0, len(level))
	for id := range level {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	nodes := make([]domain.GenealogyNode, 0, len(ids))
	for _, id := range ids {
		lot, err := getLot(ctx, s.db, id, false)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, domain.GenealogyNode{LotID: lot.ID, LotNo: lot.LotNo, ItemName: lot.ItemName, Level: level[id], Status: lot.Status})
	}

	edges := make([]domain.GenealogyEdge, 0)
	seen := map[string]bool{}
	for _, e := range fedges {
		if e.ParentLotID == nil {
			continue
		}
		key := *e.ParentLotID + ">" + e.LotID + ">" + strDeref(e.ProcessID)
		if seen[key] {
			continue
		}
		seen[key] = true
		qty := decimal.Zero
		if e.QtyFromParent != nil {
			qty = *e.QtyFromParent
		}
		edges = append(edges, domain.GenealogyEdge{
			FromLotID: *e.ParentLotID, ToLotID: e.LotID, Qty: domain.NewQty(qty),
			UOM: strDeref(e.UOM), ProcessType: strDeref(e.ProcessType), ProcessID: strDeref(e.ProcessID),
		})
	}
	for _, e := range bedges {
		if e.ChildLotID == nil {
			continue
		}
		key := e.LotID + ">" + *e.ChildLotID + ">" + strDeref(e.ProcessID)
		if seen[key] {
			continue
		}
		seen[key] = true
		qty := decimal.Zero
		if e.QtyUsed != nil {
			qty = *e.QtyUsed
		}
		edges = append(edges, domain.GenealogyEdge{
			FromLotID: e.LotID, ToLotID: *e.ChildLotID, Qty: domain.NewQty(qty),
			UOM: strDeref(e.UOM), ProcessType: strDeref(e.ProcessType), ProcessID: strDeref(e.ProcessID),
		})
	}

	return &domain.GenealogyResponse{RootLotID: root.ID, RootLotNo: root.LotNo, Nodes: nodes, Edges: edges}, nil
}

// ---------------------------------------------------------------------------
// Geri çağırma
// ---------------------------------------------------------------------------

type recallCustomerAgg struct {
	customerID  string
	qtyShipped  decimal.Decimal
	qtyReturned decimal.Decimal
}

// aggregateCustomersForLot: bu lot için müşteri bazında sevk edilen (satış satırları + sipariş
// bağlantısız sevkiyatların birleşiminden) ve iade edilen miktarı döner. Sıralı iki sorgu (açık
// `rows` üst üste binmez).
func aggregateCustomersForLot(ctx context.Context, tx *sql.Tx, lotID string) ([]recallCustomerAgg, error) {
	shipped := map[string]decimal.Decimal{}
	rows, err := tx.QueryContext(ctx,
		`SELECT customer_id, SUM(qty) FROM (
		   SELECT so.customer_id AS customer_id, sl.qty AS qty
		   FROM sales_line sl JOIN sales_order so ON so.id = sl.sales_order_id WHERE sl.lot_id = ?
		   UNION ALL
		   SELECT sh.customer_id AS customer_id, shl.qty AS qty
		   FROM shipment_line shl JOIN shipment sh ON sh.id = shl.shipment_id
		   WHERE shl.lot_id = ? AND sh.sales_order_id IS NULL
		 ) x GROUP BY customer_id`, lotID, lotID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var custID string
		var qty decimal.Decimal
		if err := rows.Scan(&custID, &qty); err != nil {
			rows.Close()
			return nil, err
		}
		shipped[custID] = qty
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	returned := map[string]decimal.Decimal{}
	rows2, err := tx.QueryContext(ctx, `SELECT customer_id, COALESCE(SUM(qty),0) FROM customer_return WHERE lot_id = ? GROUP BY customer_id`, lotID)
	if err != nil {
		return nil, err
	}
	for rows2.Next() {
		var custID string
		var qty decimal.Decimal
		if err := rows2.Scan(&custID, &qty); err != nil {
			rows2.Close()
			return nil, err
		}
		returned[custID] = qty
	}
	if err := rows2.Err(); err != nil {
		rows2.Close()
		return nil, err
	}
	rows2.Close()

	ids := map[string]bool{}
	for id := range shipped {
		ids[id] = true
	}
	for id := range returned {
		ids[id] = true
	}
	out := make([]recallCustomerAgg, 0, len(ids))
	for id := range ids {
		out = append(out, recallCustomerAgg{customerID: id, qtyShipped: shipped[id], qtyReturned: returned[id]})
	}
	return out, nil
}

// CreateRecall: TEK transaction'da -- trace-forward çalıştırılır, kök + türeyen TÜM lotlar
// recall_lot'a yazılır ve bloke edilir (status='recalled'), her lot için müşteri×lot bazında
// recall_customer satırları oluşturulur. Aynı lot için AÇIK bir recall varsa 409 (mevcut recall_no
// mesajda).
func (s *Store) CreateRecall(ctx context.Context, req domain.CreateRecallRequest) (*domain.Recall, error) {
	var recallID string
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		if _, err := getLot(ctx, tx, req.RootLotID, false); err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return domain.Invalid("root_lot_id bulunamadi")
			}
			return err
		}

		var existingNo string
		err := tx.QueryRowContext(ctx, `SELECT recall_no FROM recall WHERE root_lot_id = ? AND status = 'open' FOR UPDATE`, req.RootLotID).Scan(&existingNo)
		if err == nil {
			return domain.Conflict("bu lot icin acik bir geri cagirma zaten var: %s", existingNo)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		edges, err := fetchForwardEdges(ctx, tx, req.RootLotID)
		if err != nil {
			return err
		}
		type nodeInfo struct {
			lotID, path string
			depth       int
		}
		seen := map[string]bool{}
		var nodeList []nodeInfo
		for _, e := range edges {
			if seen[e.LotID] {
				continue
			}
			seen[e.LotID] = true
			nodeList = append(nodeList, nodeInfo{e.LotID, e.Path, e.Depth})
		}

		recallID = domain.NewID()
		recallNo, err := nextSequenceNo(ctx, tx, "RCL")
		if err != nil {
			return err
		}
		openedAt := time.Now().UTC()
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO recall (id, recall_no, root_lot_id, reason, status, opened_at) VALUES (?, ?, ?, ?, 'open', ?)`,
			recallID, recallNo, req.RootLotID, req.Reason, openedAt); err != nil {
			return mapErr(err)
		}

		for _, n := range nodeList {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO recall_lot (id, recall_id, lot_id, depth, path) VALUES (?, ?, ?, ?, ?)`,
				domain.NewID(), recallID, n.lotID, n.depth, n.path); err != nil {
				return mapErr(err)
			}
			blockReason := recallNo + ": " + req.Reason
			if _, err := setLotStatusInTx(ctx, tx, n.lotID, "recalled", &blockReason); err != nil {
				return err
			}

			aggs, err := aggregateCustomersForLot(ctx, tx, n.lotID)
			if err != nil {
				return err
			}
			for _, a := range aggs {
				if _, err := tx.ExecContext(ctx,
					`INSERT INTO recall_customer (id, recall_id, customer_id, lot_id, qty_shipped, qty_returned, contact_status)
					 VALUES (?, ?, ?, ?, ?, ?, 'pending')`,
					domain.NewID(), recallID, a.customerID, n.lotID,
					domain.NewQty(a.qtyShipped), domain.NewQty(a.qtyReturned)); err != nil {
					return mapErr(err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetRecall(ctx, recallID)
}

const recallCols = `r.id, r.recall_no, r.root_lot_id, l.lot_no, r.reason, r.status, r.opened_at, r.closed_at, r.created_at,
       (SELECT COUNT(*) FROM recall_lot rl WHERE rl.recall_id = r.id),
       (SELECT COUNT(*) FROM recall_customer rc WHERE rc.recall_id = r.id AND rc.contact_status <> 'confirmed')`

const recallFrom = ` FROM recall r JOIN lot l ON l.id = r.root_lot_id`

func scanRecallRow(row interface{ Scan(...any) error }) (*domain.Recall, error) {
	var rc domain.Recall
	var closedAt *time.Time
	err := row.Scan(&rc.ID, &rc.RecallNo, &rc.RootLotID, &rc.RootLotNo, &rc.Reason, &rc.Status,
		&rc.OpenedAt, &closedAt, &rc.CreatedAt, &rc.AffectedLotCount, &rc.PendingCustomerCount)
	if err != nil {
		return nil, mapErr(err)
	}
	rc.ClosedAt = closedAt
	return &rc, nil
}

func (s *Store) ListRecalls(ctx context.Context, p Page) ([]domain.Recall, error) {
	limit, offset := p.normalized()
	rows, err := s.db.QueryContext(ctx, `SELECT `+recallCols+recallFrom+` ORDER BY r.created_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Recall{}
	for rows.Next() {
		r, err := scanRecallRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

func loadRecallLots(ctx context.Context, q DBTX, recallID string) ([]domain.RecallLot, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT rl.lot_id, l.lot_no, i.name, rl.depth, rl.path, l.status
		 FROM recall_lot rl JOIN lot l ON l.id = rl.lot_id JOIN item i ON i.id = l.item_id
		 WHERE rl.recall_id = ? ORDER BY rl.depth, l.lot_no`, recallID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.RecallLot{}
	for rows.Next() {
		var l domain.RecallLot
		if err := rows.Scan(&l.LotID, &l.LotNo, &l.ItemName, &l.Depth, &l.Path, &l.Status); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func loadRecallCustomers(ctx context.Context, q DBTX, recallID string) ([]domain.RecallCustomerView, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT rc.id, rc.customer_id, c.name, c.phone, c.email, rc.lot_id, l.lot_no,
		        rc.qty_shipped, rc.qty_returned, rc.contact_status, rc.notes,
		        EXISTS (SELECT 1 FROM sales_line sl WHERE sl.lot_id = rc.lot_id AND sl.recall_violation = TRUE)
		 FROM recall_customer rc JOIN customer c ON c.id = rc.customer_id JOIN lot l ON l.id = rc.lot_id
		 WHERE rc.recall_id = ? ORDER BY c.name, l.lot_no`, recallID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.RecallCustomerView{}
	for rows.Next() {
		var v domain.RecallCustomerView
		if err := rows.Scan(&v.ID, &v.CustomerID, &v.CustomerName, &v.CustomerPhone, &v.CustomerEmail, &v.LotID, &v.LotNo,
			&v.QtyShipped, &v.QtyReturned, &v.ContactStatus, &v.Notes, &v.HasViolation); err != nil {
			return nil, err
		}
		v.RemainingRisk = domain.NewQty(v.QtyShipped.Decimal.Sub(v.QtyReturned.Decimal))
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for i := range out { // ikinci kademe kayitlari: rows kapandiktan SONRA (ayni tx/baglanti)
		entries, err := loadDownstream(ctx, q, out[i].CustomerID, []string{out[i].LotID})
		if err != nil {
			return nil, err
		}
		out[i].Downstream = entries
		out[i].DownstreamKnown, out[i].DownstreamNote = downstreamStatus(entries, out[i].RemainingRisk.Decimal)
	}
	return out, nil
}

func (s *Store) GetRecall(ctx context.Context, id string) (*domain.Recall, error) {
	rc, err := scanRecallRow(s.db.QueryRowContext(ctx, `SELECT `+recallCols+recallFrom+` WHERE r.id = ?`, id))
	if err != nil {
		return nil, err
	}
	if rc.AffectedLots, err = loadRecallLots(ctx, s.db, id); err != nil {
		return nil, err
	}
	if rc.Customers, err = loadRecallCustomers(ctx, s.db, id); err != nil {
		return nil, err
	}
	return rc, nil
}

// PatchRecallCustomer: contact_status ve/veya notes günceller (pending -> contacted -> confirmed).
func (s *Store) PatchRecallCustomer(ctx context.Context, recallID, recallCustomerID string, req domain.PatchRecallCustomerRequest) (*domain.Recall, error) {
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		var curRecallID string
		if err := tx.QueryRowContext(ctx, `SELECT recall_id FROM recall_customer WHERE id = ? FOR UPDATE`, recallCustomerID).Scan(&curRecallID); err != nil {
			return mapErr(err)
		}
		if curRecallID != recallID {
			return domain.ErrNotFound
		}
		if req.ContactStatus != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE recall_customer SET contact_status = ? WHERE id = ?`, *req.ContactStatus, recallCustomerID); err != nil {
				return mapErr(err)
			}
		}
		if req.Notes != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE recall_customer SET notes = ? WHERE id = ?`, *req.Notes, recallCustomerID); err != nil {
				return mapErr(err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetRecall(ctx, recallID)
}

// CloseRecall: tüm müşteriler 'confirmed' değilse 422 (eksik listesiyle); aksi halde status='closed'.
func (s *Store) CloseRecall(ctx context.Context, id string) (*domain.Recall, error) {
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		var status string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM recall WHERE id = ? FOR UPDATE`, id).Scan(&status); err != nil {
			return mapErr(err)
		}
		if status == "closed" {
			return nil
		}
		rows, err := tx.QueryContext(ctx,
			`SELECT c.name, l.lot_no, rc.contact_status FROM recall_customer rc
			 JOIN customer c ON c.id = rc.customer_id JOIN lot l ON l.id = rc.lot_id
			 WHERE rc.recall_id = ? AND rc.contact_status <> 'confirmed'
			 ORDER BY c.name, l.lot_no`, id)
		if err != nil {
			return err
		}
		var pending []string
		for rows.Next() {
			var name, lotNo, st string
			if err := rows.Scan(&name, &lotNo, &st); err != nil {
				rows.Close()
				return err
			}
			pending = append(pending, fmt.Sprintf("%s (%s, durum: %s)", name, lotNo, st))
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if len(pending) > 0 {
			return domain.Invalid("tum musteriler onaylanmadan (confirmed) geri cagirma kapatilamaz: %s", strings.Join(pending, "; "))
		}
		if _, err := tx.ExecContext(ctx, `UPDATE recall SET status = 'closed', closed_at = CURRENT_TIMESTAMP(3) WHERE id = ?`, id); err != nil {
			return mapErr(err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetRecall(ctx, id)
}

// ReopenRecall: kapatılmış geri çağırmayı tekrar 'open' yapar (lotların bloke/recalled durumu
// buradan etkilenmez; onu kaldırmak için ayrı POST /lots/{id}/unblock kullanılır).
func (s *Store) ReopenRecall(ctx context.Context, id string) (*domain.Recall, error) {
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		var tmp string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM recall WHERE id = ? FOR UPDATE`, id).Scan(&tmp); err != nil {
			return mapErr(err)
		}
		_, err := tx.ExecContext(ctx, `UPDATE recall SET status = 'open', closed_at = NULL WHERE id = ?`, id)
		return mapErr(err)
	})
	if err != nil {
		return nil, err
	}
	return s.GetRecall(ctx, id)
}
