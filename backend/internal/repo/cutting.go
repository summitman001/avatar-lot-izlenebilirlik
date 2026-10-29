package repo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/mlclient"
)

// Gorev 2: kuzu parcalama ve maliyet dagitimi, uctan uca servis katmani.
//
// Akis: POST /cutting-orders (draft, toplam maliyet VE PURCHASE_TOTAL_MISMATCH/MISMATCH_RESOLUTION_MISSING
// SIPARIS OLUSTURULURKEN sabitlenir) -> PUT .../outputs (fiili agirliklar, draft iken coklu cagri,
// her cagrida her IKI yontemle (quantity/value) taze onizleme) -> POST .../close (closable degilse 422;
// tek transaction'da cutting_consume + her parca icin yeni lot + cutting_in + lot_link + prediction
// actual/error). Domain hesabi domain.AllocateCutting'tir (bkz. domain/cutting.go); bu dosya SQL/
// transaction mantigini ve ML entegrasyonunu (en iyi gayret) tasir.

// partShortCode "KUZU-BUT" -> "BUT" gibi urun kodundan parca kisa kodunu cikarir. Varsayim: bu gorev
// TEK hayvan turuyle (kuzu) sinirlidir, urun kodlari "KUZU-" onekini tasir (bkz. docs/assumptions.md).
func partShortCode(itemCode string) string {
	return strings.TrimPrefix(itemCode, "KUZU-")
}

// cuttingPartLotPrefix cikti lotu numarasinin oneki: "KUZ-BUT-YYYY-NNN" gibi (atomik number_sequence).
func cuttingPartLotPrefix(itemCode string) string {
	return "KUZ-" + partShortCode(itemCode)
}

// partMLKey ML'in /predict/cutting yanitindaki parca anahtari (ASCII, kucuk harf; ör. "but","kiyma",
// "fire") ile recetedeki urun kodunu eslestirir (ml/app/datasets.py CUTTING_PARTS ile ayni kume).
func partMLKey(itemCode string) string {
	return strings.ToLower(partShortCode(itemCode))
}

// ---------------------------------------------------------------------------
// Recete (cutting_recipe + cutting_recipe_line)
// ---------------------------------------------------------------------------

const cuttingRecipeCols = `id, code, name, animal_type, created_at`

func scanCuttingRecipe(row interface{ Scan(...any) error }) (*domain.CuttingRecipe, error) {
	var r domain.CuttingRecipe
	if err := row.Scan(&r.ID, &r.Code, &r.Name, &r.AnimalType, &r.CreatedAt); err != nil {
		return nil, mapErr(err)
	}
	return &r, nil
}

func loadCuttingRecipeLines(ctx context.Context, q DBTX, recipeID string) ([]domain.CuttingRecipeLine, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT crl.part_item_id, it.code, it.name, crl.expected_ratio, crl.reference_sale_price, crl.is_by_product, crl.is_waste
		 FROM cutting_recipe_line crl JOIN item it ON it.id = crl.part_item_id
		 WHERE crl.recipe_id = ? ORDER BY crl.created_at`, recipeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.CuttingRecipeLine{}
	for rows.Next() {
		var l domain.CuttingRecipeLine
		if err := rows.Scan(&l.PartItemID, &l.PartItemCode, &l.PartItemName, &l.ExpectedRatio,
			&l.ReferenceSalePrice, &l.IsByProduct, &l.IsWaste); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func getCuttingRecipeDetail(ctx context.Context, q DBTX, id string) (*domain.CuttingRecipe, error) {
	r, err := scanCuttingRecipe(q.QueryRowContext(ctx, `SELECT `+cuttingRecipeCols+` FROM cutting_recipe WHERE id = ?`, id))
	if err != nil {
		return nil, err
	}
	if r.Lines, err = loadCuttingRecipeLines(ctx, q, id); err != nil {
		return nil, err
	}
	return r, nil
}

func (s *Store) GetCuttingRecipe(ctx context.Context, id string) (*domain.CuttingRecipe, error) {
	return getCuttingRecipeDetail(ctx, s.db, id)
}

func (s *Store) ListCuttingRecipes(ctx context.Context, p Page) ([]domain.CuttingRecipe, error) {
	limit, offset := p.normalized()
	rows, err := s.db.QueryContext(ctx, `SELECT `+cuttingRecipeCols+` FROM cutting_recipe ORDER BY code LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.CuttingRecipe{}
	for rows.Next() {
		r, err := scanCuttingRecipe(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		lines, err := loadCuttingRecipeLines(ctx, s.db, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Lines = lines
	}
	return out, nil
}

// writeCuttingRecipeLines yeni satirlari ekler (recipeID icin ESKI satirlarin onceden silindigini
// varsayar; UpdateCuttingRecipe bunu kendisi yapar). Her parca urununun var oldugunu dogrular.
func (s *Store) writeCuttingRecipeLines(ctx context.Context, tx *sql.Tx, recipeID string, lines []domain.CuttingRecipeLine) error {
	for i, l := range lines {
		if _, err := getItem(ctx, tx, l.PartItemID); errors.Is(err, domain.ErrNotFound) {
			return domain.Invalid("lines[%d].part_item_id bulunamadi", i)
		} else if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO cutting_recipe_line (id, recipe_id, part_item_id, expected_ratio, reference_sale_price, is_by_product, is_waste)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			domain.NewID(), recipeID, l.PartItemID, l.ExpectedRatio, l.ReferenceSalePrice, l.IsByProduct, l.IsWaste); err != nil {
			return mapErr(err)
		}
	}
	return nil
}

func (s *Store) CreateCuttingRecipe(ctx context.Context, in domain.CuttingRecipe) (*domain.CuttingRecipe, error) {
	id, err := resolveID(in.ID)
	if err != nil {
		return nil, err
	}
	var out *domain.CuttingRecipe
	err = s.InTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO cutting_recipe (id, code, name, animal_type) VALUES (?, ?, ?, ?)`,
			id, in.Code, in.Name, in.AnimalType); err != nil {
			return mapErr(err)
		}
		if err := s.writeCuttingRecipeLines(ctx, tx, id, in.Lines); err != nil {
			return err
		}
		var gerr error
		out, gerr = getCuttingRecipeDetail(ctx, tx, id)
		return gerr
	})
	return out, err
}

// UpdateCuttingRecipe tam guncellemedir (PUT): satirlar silinip yeniden yazilir (idempotent, siralama
// onemli degil). Var olan parcalama emirlerini ETKILEMEZ (onlar kendi toplam maliyetini/dagitimini
// SIPARIS OLUSTURULURKEN sabitlemistir).
func (s *Store) UpdateCuttingRecipe(ctx context.Context, id string, in domain.CuttingRecipe) (*domain.CuttingRecipe, error) {
	var out *domain.CuttingRecipe
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE cutting_recipe SET code = ?, name = ?, animal_type = ? WHERE id = ?`,
			in.Code, in.Name, in.AnimalType, id)
		if err != nil {
			return mapErr(err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return domain.ErrNotFound
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM cutting_recipe_line WHERE recipe_id = ?`, id); err != nil {
			return mapErr(err)
		}
		if err := s.writeCuttingRecipeLines(ctx, tx, id, in.Lines); err != nil {
			return err
		}
		var gerr error
		out, gerr = getCuttingRecipeDetail(ctx, tx, id)
		return gerr
	})
	return out, err
}

// ---------------------------------------------------------------------------
// Parcalama emri: okuma
// ---------------------------------------------------------------------------

// cuttingOrderCols: giris lotunun declared_line_total/initial_qty'si de JOIN'lenir ki liste gorunumu
// de (detay gibi) ComputedTotalCost/DeclaredTotalCost'u ekstra sorgu atmadan hesaplayabilsin.
const cuttingOrderCols = `co.id, co.order_no, co.recipe_id, cr.code, cr.name,
       co.input_lot_id, l.lot_no, it.code, it.name, l.unit_cost, l.initial_qty, prl.declared_line_total,
       co.input_weight_kg, co.total_input_cost,
       co.allocation_method, co.cost_basis, co.resolution_note,
       co.status, co.cut_at, co.balance_diff_kg, co.closable, co.closed_at, co.created_at`

const cuttingOrderFrom = ` FROM cutting_order co
JOIN cutting_recipe cr ON cr.id = co.recipe_id
JOIN lot l ON l.id = co.input_lot_id
JOIN item it ON it.id = l.item_id
LEFT JOIN purchase_receipt_line prl ON prl.lot_id = l.id`

func scanCuttingOrder(row interface{ Scan(...any) error }) (*domain.CuttingOrder, error) {
	var o domain.CuttingOrder
	var lotInitialQty domain.Qty
	var declaredLineTotal *domain.Amount
	err := row.Scan(&o.ID, &o.OrderNo, &o.RecipeID, &o.RecipeCode, &o.RecipeName,
		&o.InputLotID, &o.InputLotNo, &o.InputItemCode, &o.InputItemName, &o.UnitCostPerKg, &lotInitialQty, &declaredLineTotal,
		&o.InputWeightKg, &o.TotalInputCost,
		&o.AllocationMethod, &o.CostBasis, &o.ResolutionNote,
		&o.Status, &o.CutAt, &o.BalanceDiffKg, &o.Closable, &o.ClosedAt, &o.CreatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	o.ComputedTotalCost = domain.NewAmount(domain.LineTotal(o.InputWeightKg.Decimal, o.UnitCostPerKg.Decimal))
	if declaredLineTotal != nil && lotInitialQty.IsPositive() {
		prorated := declaredLineTotal.Decimal.Mul(o.InputWeightKg.Decimal).Div(lotInitialQty.Decimal)
		d := domain.NewAmount(prorated)
		o.DeclaredTotalCost = &d
	}
	// cost_basis_used: computed_total_cost HER ZAMAN mevcuttur (lot.unit_cost'tan), bu yuzden
	// domain.ResolveCostBasis'in secimi basittir -- CostBasis verilmisse o, verilmemisse "computed"
	// (bkz. domain.ResolveCostBasis). Ayrica cagirmaya gerek yok.
	o.CostBasisUsed = domain.BasisComputed
	if o.CostBasis != nil {
		o.CostBasisUsed = *o.CostBasis
	}
	o.Warnings = []string{} // liste gorunumunde taze hesap yok; detay (getCuttingOrderDetail) doldurur
	return &o, nil
}

type cuttingOutputRow struct {
	ActualWeightKg domain.Qty
	OutputLotID    *string
	OutputLotNo    *string
	AllocatedCost  *domain.Amount
	UnitCost       *domain.Amount
}

func loadCuttingOutputRows(ctx context.Context, q DBTX, orderID string) (map[string]cuttingOutputRow, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT co.part_item_id, co.actual_weight_kg, co.output_lot_id, l.lot_no, co.allocated_cost, co.unit_cost
		 FROM cutting_output co LEFT JOIN lot l ON l.id = co.output_lot_id
		 WHERE co.cutting_order_id = ?`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]cuttingOutputRow{}
	for rows.Next() {
		var partID string
		var r cuttingOutputRow
		var lotNo sql.NullString
		if err := rows.Scan(&partID, &r.ActualWeightKg, &r.OutputLotID, &lotNo, &r.AllocatedCost, &r.UnitCost); err != nil {
			return nil, err
		}
		if lotNo.Valid {
			r.OutputLotNo = &lotNo.String
		}
		out[partID] = r
	}
	return out, rows.Err()
}

// loadCuttingPrediction en son ML tahminini (varsa) API gorunumune cevirir; ML hic cevap vermediyse
// (kapaliydi/zaman asimi) nil, nil doner.
func loadCuttingPrediction(ctx context.Context, q DBTX, orderID string) (*domain.CuttingPredictionView, error) {
	var modelName, modelVersion string
	var predictedJSON []byte
	err := q.QueryRowContext(ctx,
		`SELECT model_name, model_version, predicted_json FROM prediction WHERE target_type = 'cutting' AND ref_id = ? ORDER BY created_at DESC LIMIT 1`,
		orderID).Scan(&modelName, &modelVersion, &predictedJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var pred mlclient.CuttingPrediction
	if err := json.Unmarshal(predictedJSON, &pred); err != nil {
		return nil, err
	}
	partsKg := map[string]string{}
	for _, p := range pred.Parts {
		partsKg[p.Part] = p.Kg
	}
	partsKg["fire"] = pred.ExpectedFireKg
	return &domain.CuttingPredictionView{
		ModelName: modelName, ModelVersion: modelVersion, Baseline: pred.Baseline, BreedUsed: pred.BreedUsed,
		ExpectedFireKg: pred.ExpectedFireKg, ExpectedFireRatio: pred.ExpectedFireRatio, PartsKg: partsKg,
	}, nil
}

// buildCuttingPreview recete satirlari + fiili agirliklar (girilmemisse 0) uzerinden HER IKI dagitim
// yontemini de (quantity/value) hesaplar; secilen yontem (order.AllocationMethod) closable/balance_diff_kg/
// warnings'i belirler. Outputs recipeLines ile AYNI sirada doner (CloseCuttingOrder index ile eslestirir).
func buildCuttingPreview(o *domain.CuttingOrder, recipeLines []domain.CuttingRecipeLine,
	outputs map[string]cuttingOutputRow, pred *domain.CuttingPredictionView,
) ([]domain.CuttingOutputLine, *domain.CuttingAllocationPreview, *domain.Qty, bool, []string, error) {
	parts := make([]domain.Part, len(recipeLines))
	for i, rl := range recipeLines {
		kg := decimal.Zero
		if out, ok := outputs[rl.PartItemID]; ok {
			kg = out.ActualWeightKg.Decimal
		}
		parts[i] = domain.Part{Name: rl.PartItemName, Kg: kg, ReferencePrice: rl.ReferenceSalePrice.Decimal,
			IsWaste: rl.IsWaste, IsByProduct: rl.IsByProduct}
	}

	// outLines; recomputed cost/unit alanlari (AllocatedCost/UnitCost) KAPANISTA persist edilen
	// cutting_output degerleridir (asagidaki qtyResult/valResult'tan BAGIMSIZDIR) -- bu yuzden allocation
	// hesaplanamasa bile (asagi bkz.) eksiksiz doldurulabilir.
	outLines := make([]domain.CuttingOutputLine, len(recipeLines))
	for i, rl := range recipeLines {
		line := domain.CuttingOutputLine{
			PartItemID: rl.PartItemID, PartItemCode: rl.PartItemCode, PartItemName: rl.PartItemName,
			IsWaste: rl.IsWaste, IsByProduct: rl.IsByProduct, ExpectedRatio: rl.ExpectedRatio,
			ExpectedWeightKg:   domain.NewQty(domain.Round3(o.InputWeightKg.Decimal.Mul(rl.ExpectedRatio.Decimal))),
			ReferenceSalePrice: rl.ReferenceSalePrice,
		}
		if pred != nil {
			if v, ok := pred.PartsKg[partMLKey(rl.PartItemCode)]; ok {
				line.PredictedWeightKg = &v
			}
		}
		if out, ok := outputs[rl.PartItemID]; ok {
			w := out.ActualWeightKg
			line.ActualWeightKg, line.OutputLotID, line.OutputLotNo = &w, out.OutputLotID, out.OutputLotNo
			line.AllocatedCost, line.UnitCost = out.AllocatedCost, out.UnitCost
		}
		outLines[i] = line
	}

	// Hic fiili agirlik girilmemisse (ör. siparis OLUSTURULDUGU an) TUM ana parcalarin agirligi 0'dir;
	// domain.Distribute agirlik toplami sifir oldugunda hata doner (anlamli bir oranti yok). Bu durumda
	// dagitimi hic HESAPLAMAYIZ (allocation=nil); denge farki = tum giris agirligi, WEIGHT_IMBALANCE.
	anyMainPositive := false
	partsWeight := decimal.Zero
	for _, p := range parts {
		partsWeight = partsWeight.Add(p.Kg)
		if !p.IsWaste && !p.IsByProduct && p.Kg.IsPositive() {
			anyMainPositive = true
		}
	}
	if !anyMainPositive {
		basis, err := domain.ResolveCostBasis(&o.UnitCostPerKg.Decimal, declaredPtr(o), o.InputWeightKg.Decimal, o.CostBasis, o.ResolutionNote)
		if err != nil {
			return nil, nil, nil, false, nil, err
		}
		balanceDiff := domain.NewQty(o.InputWeightKg.Decimal.Sub(partsWeight))
		return outLines, nil, &balanceDiff, false, append(basis.Warnings, domain.WarnWeightImbalance), nil
	}

	run := func(method string) (*domain.CuttingResult, error) {
		res, err := domain.AllocateCutting(domain.CuttingInput{
			InputWeightKg: o.InputWeightKg.Decimal, Parts: parts, Method: method,
			UnitCostPerKg: &o.UnitCostPerKg.Decimal, DeclaredTotal: declaredPtr(o),
			CostBasis: o.CostBasis, ResolutionNote: o.ResolutionNote,
		})
		if err != nil {
			return nil, err
		}
		return &res, nil
	}
	qtyResult, err := run(domain.MethodQuantity)
	if err != nil {
		return nil, nil, nil, false, nil, err
	}
	valResult, err := run(domain.MethodValue)
	if err != nil {
		return nil, nil, nil, false, nil, err
	}
	selected := qtyResult
	if o.AllocationMethod == domain.MethodValue {
		selected = valResult
	}
	balanceDiff := domain.NewQty(decimal.RequireFromString(selected.BalanceDiffKg))

	allocation := &domain.CuttingAllocationPreview{Quantity: qtyResult, Value: valResult}
	return outLines, allocation, &balanceDiff, selected.Closable, selected.Warnings, nil
}

// declaredPtr: CuttingOrder.DeclaredTotalCost varsa iceren decimal'e isaretci (AllocateCutting/
// ResolveCostBasis'in DeclaredTotal parametresi icin), yoksa nil.
func declaredPtr(o *domain.CuttingOrder) *decimal.Decimal {
	if o.DeclaredTotalCost == nil {
		return nil
	}
	return &o.DeclaredTotalCost.Decimal
}

func getCuttingOrderDetail(ctx context.Context, q DBTX, id string) (*domain.CuttingOrder, error) {
	o, err := scanCuttingOrder(q.QueryRowContext(ctx, `SELECT `+cuttingOrderCols+cuttingOrderFrom+` WHERE co.id = ?`, id))
	if err != nil {
		return nil, err
	}
	recipeLines, err := loadCuttingRecipeLines(ctx, q, o.RecipeID)
	if err != nil {
		return nil, err
	}
	outputs, err := loadCuttingOutputRows(ctx, q, id)
	if err != nil {
		return nil, err
	}
	pred, err := loadCuttingPrediction(ctx, q, id)
	if err != nil {
		return nil, err
	}
	o.MLPrediction = pred
	outLines, allocation, balanceDiff, closable, warnings, err := buildCuttingPreview(o, recipeLines, outputs, pred)
	if err != nil {
		return nil, err
	}
	o.Outputs, o.Allocation, o.BalanceDiffKg, o.Closable, o.Warnings = outLines, allocation, balanceDiff, closable, warnings
	if o.AnomalyAlerts, err = loadAnomalyAlerts(ctx, q, "cutting_order", id); err != nil {
		return nil, err
	}
	return o, nil
}

func (s *Store) GetCuttingOrder(ctx context.Context, id string) (*domain.CuttingOrder, error) {
	return getCuttingOrderDetail(ctx, s.db, id)
}

type CuttingOrderFilter struct {
	Status     string
	InputLotID string
	Page       Page
}

// ListCuttingOrders girdilerin/onizlemenin AGIR yeniden hesabini icermez (uretim siparisi listesiyle
// ayni desen); closable/balance_diff_kg en son yazma isleminde (outputs guncelleme/kapanis) persist
// edilen degerlerdir.
func (s *Store) ListCuttingOrders(ctx context.Context, f CuttingOrderFilter) ([]domain.CuttingOrder, error) {
	var b query
	if f.Status != "" {
		b.add(`co.status = ?`, f.Status)
	}
	if f.InputLotID != "" {
		b.add(`co.input_lot_id = ?`, f.InputLotID)
	}
	limit, offset := f.Page.normalized()
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+cuttingOrderCols+cuttingOrderFrom+b.sql()+` ORDER BY co.created_at DESC LIMIT ? OFFSET ?`,
		append(b.args, limit, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.CuttingOrder{}
	for rows.Next() {
		o, err := scanCuttingOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *o)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// ML tahmini (en iyi gayret)
// ---------------------------------------------------------------------------

// predictOnCuttingCreate siparis olusturduktan SONRA cagrilir: ML erisilemezse sessizce atlanir,
// siparis olusturma BASARISIZ OLMAZ (bkz. docs/assumptions.md).
func (s *Store) predictOnCuttingCreate(ctx context.Context, o *domain.CuttingOrder) {
	if s.ml == nil {
		return
	}
	carcassKg := o.InputWeightKg.StringFixed(domain.QtyScale)
	pred, err := s.ml.PredictCutting(ctx, mlclient.CuttingPredictionInput{CarcassKg: carcassKg})
	if err != nil {
		slog.Warn("ml parcalama tahmini alinamadi, siparis tahminsiz olusturuldu", "order_no", o.OrderNo, "err", err)
		return
	}
	predictedJSON, _ := json.Marshal(pred)
	inputJSON, _ := json.Marshal(map[string]string{"carcass_kg": carcassKg})
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO prediction (id, model_name, model_version, target_type, ref_id, input_json, predicted_json)
		 VALUES (?, ?, ?, 'cutting', ?, ?, ?)`,
		domain.NewID(), pred.ModelName, pred.ModelVersion, o.ID, string(inputJSON), string(predictedJSON)); err != nil {
		slog.Warn("ml parcalama tahmini kaydedilemedi", "order_no", o.OrderNo, "err", err)
	}
}

// recordCuttingPredictionActual kapanista (en iyi gayret) fiili agirliklari EN GUNCEL tahminle
// karsilastirip AYNI tahmin satirina (actual_json/error_json) yazar. Tahmin hic alinmamissa sessizce
// atlanir.
func (s *Store) recordCuttingPredictionActual(ctx context.Context, tx *sql.Tx, orderID string, outputs []domain.CuttingOutputLine, selected *domain.CuttingResult) error {
	var predID string
	var predictedJSON []byte
	err := tx.QueryRowContext(ctx,
		`SELECT id, predicted_json FROM prediction WHERE target_type = 'cutting' AND ref_id = ? ORDER BY created_at DESC LIMIT 1`,
		orderID).Scan(&predID, &predictedJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var pred mlclient.CuttingPrediction
	if err := json.Unmarshal(predictedJSON, &pred); err != nil {
		return err
	}
	predictedKg := map[string]decimal.Decimal{}
	for _, p := range pred.Parts {
		if d, err := decimal.NewFromString(p.Kg); err == nil {
			predictedKg[p.Part] = d
		}
	}
	if d, err := decimal.NewFromString(pred.ExpectedFireKg); err == nil {
		predictedKg["fire"] = d
	}

	actual := map[string]string{}
	diffs := map[string]map[string]string{}
	for i, out := range outputs {
		key := partMLKey(out.PartItemCode)
		actualKg := decimal.RequireFromString(selected.Parts[i].Kg)
		actual[key] = actualKg.StringFixed(domain.QtyScale)
		predKg, ok := predictedKg[key]
		if !ok {
			continue
		}
		diff := actualKg.Sub(predKg)
		pct := decimal.Zero
		if predKg.IsPositive() {
			pct = domain.Round2(diff.Abs().Mul(decimal.NewFromInt(100)).Div(predKg))
		}
		diffs[key] = map[string]string{"diff_kg": diff.StringFixed(domain.QtyScale), "diff_pct": pct.StringFixed(2)}
	}
	actualJSON, _ := json.Marshal(actual)
	errJSON, _ := json.Marshal(diffs)
	_, err = tx.ExecContext(ctx, `UPDATE prediction SET actual_json = ?, error_json = ? WHERE id = ?`,
		string(actualJSON), string(errJSON), predID)
	return err
}

// ---------------------------------------------------------------------------
// Yazma: create / outputs / close
// ---------------------------------------------------------------------------

// CreateCuttingOrder: giris lotu aktif ve yeterli bakiyeli olmali; toplam maliyet VE PURCHASE_TOTAL_MISMATCH/
// MISMATCH_RESOLUTION_MISSING durumu BURADA (domain.ResolveCostBasis ile) sabitlenir, cutting_order'a
// yazilir -- sonraki outputs/close adimlari bu degerleri DEGISTIRMEZ.
func (s *Store) CreateCuttingOrder(ctx context.Context, req domain.CreateCuttingOrderRequest) (*domain.CuttingOrder, error) {
	id, err := resolveIDPtr(req.ID)
	if err != nil {
		return nil, err
	}
	err = s.InTx(ctx, func(tx *sql.Tx) error {
		if ok, err := exists(ctx, tx, "cutting_recipe", req.RecipeID); err != nil {
			return err
		} else if !ok {
			return domain.Invalid("recipe_id bulunamadi")
		}
		lot, err := getLot(ctx, tx, req.InputLotID, true)
		if errors.Is(err, domain.ErrNotFound) {
			return domain.Invalid("input_lot_id bulunamadi")
		} else if err != nil {
			return err
		}
		if lot.Status != "active" {
			return domain.Invalid("lot %s aktif degil (durum: %s), parcalanamaz", lot.LotNo, lot.Status)
		}
		onHand := decimal.Zero
		if lot.QtyOnHand != nil {
			onHand = lot.QtyOnHand.Decimal
		}
		if onHand.LessThan(req.InputWeightKg.Decimal) {
			return domain.Conflict("lot stogu yetersiz (lot %s, eldeki %s, istenen %s)",
				lot.LotNo, onHand.StringFixed(domain.QtyScale), req.InputWeightKg.StringFixed(domain.QtyScale))
		}

		var declaredTotal *decimal.Decimal
		if lot.Purchase != nil && lot.Purchase.DeclaredLineTotal != nil && lot.InitialQty.IsPositive() {
			prorated := lot.Purchase.DeclaredLineTotal.Decimal.Mul(req.InputWeightKg.Decimal).Div(lot.InitialQty.Decimal)
			declaredTotal = &prorated
		}
		basis, err := domain.ResolveCostBasis(&lot.UnitCost.Decimal, declaredTotal, req.InputWeightKg.Decimal, req.CostBasis, req.ResolutionNote)
		if err != nil {
			return err
		}

		orderNo := ""
		if req.OrderNo != nil {
			orderNo = *req.OrderNo
		} else if orderNo, err = nextSequenceNo(ctx, tx, "CUT"); err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO cutting_order (id, order_no, recipe_id, input_lot_id, input_weight_kg, total_input_cost,
			                            allocation_method, cost_basis, resolution_note, status, cut_at, closable)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'draft', ?, FALSE)`,
			id, orderNo, req.RecipeID, req.InputLotID, req.InputWeightKg, domain.NewAmount(basis.TotalCost),
			req.AllocationMethod, req.CostBasis, req.ResolutionNote, req.CutAt); err != nil {
			return mapErr(err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out, err := s.GetCuttingOrder(ctx, id)
	if err != nil {
		return nil, err
	}
	s.predictOnCuttingCreate(ctx, out) // en iyi gayret; basarisizlik siparis sonucunu etkilemez
	return s.GetCuttingOrder(ctx, id)
}

// recomputeCuttingPreview taze onizlemeyi (buildCuttingPreview) hesaplayip closable/balance_diff_kg'yi
// cutting_order'a yazar (liste gorunumu bu kolonlari okur; detay/GET her zaman taze hesaplar).
func (s *Store) recomputeCuttingPreview(ctx context.Context, tx *sql.Tx, orderID string) (*domain.CuttingOrder, error) {
	o, err := getCuttingOrderDetail(ctx, tx, orderID)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE cutting_order SET balance_diff_kg = ?, closable = ? WHERE id = ?`,
		o.BalanceDiffKg, o.Closable, orderID); err != nil {
		return nil, mapErr(err)
	}
	return o, nil
}

// UpdateCuttingOutputs fiili agirliklari UPSERT eder (draft iken birden cok kez cagrilabilir) ve her
// cagrida HER IKI yontemle (quantity/value) taze onizleme hesaplar.
func (s *Store) UpdateCuttingOutputs(ctx context.Context, orderID string, req domain.UpdateCuttingOutputsRequest) (*domain.CuttingOrder, error) {
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		var status, recipeID string
		if err := tx.QueryRowContext(ctx, `SELECT status, recipe_id FROM cutting_order WHERE id = ? FOR UPDATE`, orderID).
			Scan(&status, &recipeID); err != nil {
			return mapErr(err)
		}
		if err := domain.CanUpdateCuttingOutputs(status); err != nil {
			return err
		}

		recipeLines, err := loadCuttingRecipeLines(ctx, tx, recipeID)
		if err != nil {
			return err
		}
		refPrice := map[string]domain.Amount{}
		for _, rl := range recipeLines {
			refPrice[rl.PartItemID] = rl.ReferenceSalePrice
		}

		for i, line := range req.Outputs {
			if _, ok := refPrice[line.PartItemID]; !ok {
				return domain.Invalid("outputs[%d].part_item_id recetede yok", i)
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO cutting_output (id, cutting_order_id, part_item_id, actual_weight_kg, sale_price_basis)
				 VALUES (?, ?, ?, ?, ?) AS new
				 ON DUPLICATE KEY UPDATE actual_weight_kg = new.actual_weight_kg`,
				domain.NewID(), orderID, line.PartItemID, line.ActualWeightKg, refPrice[line.PartItemID]); err != nil {
				return mapErr(err)
			}
		}
		o, err := s.recomputeCuttingPreview(ctx, tx, orderID)
		if err != nil {
			return err
		}
		// KUTLE_DENGESI: ML'den bagimsiz, her zaman calisan kural tabanli kontrol (bkz. anomaly.go).
		return checkMassBalanceRule(ctx, tx, orderID, o.OrderNo, o.BalanceDiffKg)
	})
	if err != nil {
		return nil, err
	}
	return s.GetCuttingOrder(ctx, orderID)
}

// CloseCuttingOrder: closable degilse 422 (uyarilar listelenir). Tek transaction'da giris lotundan
// cutting_consume, HER cikan parca (actual_weight_kg > 0) icin yeni lot (KUZ-{parca}-YYYY-NNN) +
// cutting_in + lot_link (parent=giris lotu; Gorev 3 izlemesi bunun uzerinden calisir) + cutting_output
// guncellemesi (output_lot_id/allocated_cost/unit_cost) + prediction actual/error.
func (s *Store) CloseCuttingOrder(ctx context.Context, orderID string) (*domain.CuttingOrder, error) {
	var out *domain.CuttingOrder
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		var status, inputLotID, allocationMethod string
		var inputWeightKg domain.Qty
		if err := tx.QueryRowContext(ctx,
			`SELECT status, input_lot_id, allocation_method, input_weight_kg FROM cutting_order WHERE id = ? FOR UPDATE`, orderID).
			Scan(&status, &inputLotID, &allocationMethod, &inputWeightKg); err != nil {
			return mapErr(err)
		}
		if err := domain.CanCloseCutting(status); err != nil {
			return err
		}

		o, err := getCuttingOrderDetail(ctx, tx, orderID)
		if err != nil {
			return err
		}
		if !o.Closable {
			return domain.Invalid("parcalama kapatilamaz, uyarilar: %s", strings.Join(o.Warnings, ", "))
		}
		selected := o.Allocation.Quantity
		if allocationMethod == domain.MethodValue {
			selected = o.Allocation.Value
		}

		srcWh, err := s.lotSourceWarehouse(ctx, tx, inputLotID, inputWeightKg.Decimal)
		if err != nil {
			return err
		}
		inputLot, err := getLot(ctx, tx, inputLotID, false)
		if err != nil {
			return err
		}

		if _, err := insertMovement(ctx, tx, domain.StockMovement{
			LotID: inputLotID, WarehouseID: srcWh, MovementType: "cutting_consume",
			Qty: domain.NewQty(inputWeightKg.Decimal.Neg()), UnitCost: inputLot.UnitCost,
			RefType: "cutting_order", RefID: orderID,
		}); err != nil {
			return err
		}
		if err := appendLotBalance(ctx, tx, inputLotID); err != nil {
			return err
		}

		for i, part := range selected.Parts {
			rl := o.Outputs[i] // buildCuttingPreview Outputs'u recipeLines ile AYNI sirada doner
			actualKg, perr := decimal.NewFromString(part.Kg)
			if perr != nil {
				return perr
			}
			if !actualKg.IsPositive() {
				continue // bu parcalamada hic cikmadi (ornek: girilmemis/0 yan urun)
			}

			lotNo, err := nextSequenceNo(ctx, tx, cuttingPartLotPrefix(rl.PartItemCode))
			if err != nil {
				return err
			}
			outputLotID := domain.NewID()
			unitCost := domain.NewAmount(decimal.RequireFromString(part.UnitCost))
			allocatedCost := domain.NewAmount(decimal.RequireFromString(part.AllocatedCost))
			partItem, err := getItem(ctx, tx, rl.PartItemID)
			if err != nil {
				return err
			}
			today := domain.Today()

			if _, err := tx.ExecContext(ctx,
				`INSERT INTO lot (id, lot_no, item_id, supplier_id, origin_type, origin_id, received_or_produced_at,
				                  expiry_date, initial_qty, uom, unit_cost, status)
				 VALUES (?, ?, ?, ?, 'cutting', ?, ?, ?, ?, 'kg', ?, 'active')`,
				outputLotID, lotNo, rl.PartItemID, inputLot.SupplierID, orderID, today,
				domain.ExpiryFrom(nil, today, partItem.ShelfLifeDays), domain.NewQty(actualKg), unitCost); err != nil {
				return mapErr(err)
			}
			if _, err := insertMovement(ctx, tx, domain.StockMovement{
				LotID: outputLotID, WarehouseID: srcWh, MovementType: "cutting_in",
				Qty: domain.NewQty(actualKg), UnitCost: unitCost, RefType: "cutting_order", RefID: orderID,
			}); err != nil {
				return err
			}
			// hareketten SONRA ve bakiyeli: cihaz yeni lotun stogunu pull'dan ogrenir (docs/sync.md)
			outLot, err := getLot(ctx, tx, outputLotID, true)
			if err != nil {
				return err
			}
			if err := appendChange(ctx, tx, "lot", outputLotID, "upsert", outLot); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO lot_link (id, parent_lot_id, child_lot_id, qty_from_parent, uom, process_type, process_id)
				 VALUES (?, ?, ?, ?, 'kg', 'cutting', ?)`,
				domain.NewID(), inputLotID, outputLotID, domain.NewQty(actualKg), orderID); err != nil {
				return mapErr(err)
			}
			if _, err := tx.ExecContext(ctx,
				`UPDATE cutting_output SET output_lot_id = ?, allocated_cost = ?, unit_cost = ? WHERE cutting_order_id = ? AND part_item_id = ?`,
				outputLotID, allocatedCost, unitCost, orderID, rl.PartItemID); err != nil {
				return mapErr(err)
			}
		}

		if err := s.recordCuttingPredictionActual(ctx, tx, orderID, o.Outputs, selected); err != nil {
			slog.Warn("parcalama tahmin/gercek karsilastirmasi yazilamadi", "order_id", orderID, "err", err)
		}

		if _, err := tx.ExecContext(ctx,
			`UPDATE cutting_order SET status = 'closed', closed_at = CURRENT_TIMESTAMP(3), balance_diff_kg = ?, closable = ? WHERE id = ?`,
			o.BalanceDiffKg, o.Closable, orderID); err != nil {
			return mapErr(err)
		}

		out, err = getCuttingOrderDetail(ctx, tx, orderID)
		return err
	})
	if err == nil {
		s.detectCuttingAnomaly(ctx, orderID) // en iyi gayret, commit sonrasi (bkz. anomaly.go)
	}
	return out, err
}
