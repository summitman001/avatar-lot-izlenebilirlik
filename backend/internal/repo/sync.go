package repo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/go-sql-driver/mysql"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
)

var syncActions = map[string]bool{"create": true, "update": true, "delete": true}

// errDuplicateOp / errRejectOp: pushOne'in InTx closure'ından dönen SENTINEL hatalardır; InTx bunlari
// gorunce transaction'i geri alir (hicbir yan etki commitlenmez), pushOne disarida bunlari yakalayip
// uygun SyncResult'a cevirir.
var (
	errDuplicateOp = errors.New("sync: op_id zaten islendi")
	errRejectOp    = errors.New("sync: operasyon reddedildi")
)

func (s *Store) ServerSeq(ctx context.Context) (int64, error) {
	var seq int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) FROM change_log`).Scan(&seq)
	return seq, err
}

// Push cihazdan gelen islemleri SIRAYLA isler; her islem KENDI transaction'indadir (bir operasyonun
// hatasi digerlerini etkilemez). Desteklenen entity/action: sales_order/create, customer_return/create.
func (s *Store) Push(ctx context.Context, req domain.SyncPushRequest) (*domain.SyncPushResponse, error) {
	resp := &domain.SyncPushResponse{Results: make([]domain.SyncResult, 0, len(req.Operations))}
	for _, op := range req.Operations {
		res, err := s.pushOne(ctx, req.DeviceID, op)
		if err != nil {
			return nil, err
		}
		resp.Results = append(resp.Results, res)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE device SET last_sync_at = CURRENT_TIMESTAMP(3) WHERE id = ?`, req.DeviceID); err != nil {
		return nil, err
	}
	seq, err := s.ServerSeq(ctx)
	if err != nil {
		return nil, err
	}
	resp.ServerSeq = seq
	return resp, nil
}

// pushOperationResult: applySyncOperation'in basarili (applied/flagged) sonucudur.
type pushOperationResult struct {
	Status  string
	Message string
}

// pushOne bir islemi isler:
//  1. Bicim kontrolleri (op_id UUIDv7, entity/action/payload) basarisizsa -> "rejected", HICBIR KAYIT YAZILMAZ.
//  2. Aksi halde TEK transaction acilir: once processed_request satiri 'processing' durumuyla REZERVE
//     edilir (op_id PK'si sayesinde es zamanli ayni op_id'ler burada serilesir); ayni op_id ikinci kez
//     gelirse INSERT 1062 ile carpisir -> "duplicate" (ilk sonuc processed_request'ten okunur).
//  3. Rezervasyon basariliysa payload islenir (applySyncOperation); gecerli bir is kurali ihlali
//     (domain.ValidationError/ConflictError) bulunursa TUM transaction geri alinir -> "rejected"
//     (rezervasyon da dahil hicbir kayit kalmaz, bkz. spec "hicbir kayit yazilmaz").
//  4. Basariliysa processed_request UPDATE edilip COMMIT edilir -> applied/flagged.
func (s *Store) pushOne(ctx context.Context, deviceID string, op domain.SyncOperation) (domain.SyncResult, error) {
	reject := func(msg string) (domain.SyncResult, error) {
		return domain.SyncResult{OpID: op.OpID, Status: domain.SyncRejected, Message: msg}, nil
	}
	if _, err := domain.ParseID(op.OpID); err != nil {
		return reject("op_id gecerli bir UUIDv7 olmali")
	}
	if op.Entity == "" || !syncActions[op.Action] {
		return reject("entity zorunlu, action create|update|delete olmali")
	}
	if len(op.Payload) == 0 || !json.Valid(op.Payload) {
		return reject("payload gecerli JSON olmali")
	}

	var result pushOperationResult
	var rejectMsg string
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO processed_request (op_id, device_id, status, response_json) VALUES (?, ?, 'processing', JSON_OBJECT())`,
			op.OpID, deviceID); err != nil {
			var me *mysql.MySQLError
			if errors.As(err, &me) && me.Number == errDupEntry {
				return errDuplicateOp
			}
			return err
		}

		res, err := s.applySyncOperation(ctx, tx, deviceID, op)
		if err != nil {
			var ve *domain.ValidationError
			var ce *domain.ConflictError
			if errors.As(err, &ve) {
				rejectMsg = ve.Msg
				return errRejectOp
			}
			if errors.As(err, &ce) {
				rejectMsg = ce.Msg
				return errRejectOp
			}
			return err
		}
		result = *res

		respJSON, _ := json.Marshal(map[string]string{"message": result.Message})
		_, err = tx.ExecContext(ctx, `UPDATE processed_request SET status = ?, response_json = ? WHERE op_id = ?`,
			result.Status, string(respJSON), op.OpID)
		return err
	})

	switch {
	case errors.Is(err, errDuplicateOp):
		var prev string
		if qerr := s.db.QueryRowContext(ctx, `SELECT status FROM processed_request WHERE op_id = ?`, op.OpID).Scan(&prev); qerr != nil {
			return domain.SyncResult{}, qerr
		}
		return domain.SyncResult{OpID: op.OpID, Status: domain.SyncDuplicate,
			Message: "op_id daha once islendi (ilk sonuc: " + prev + ")"}, nil
	case errors.Is(err, errRejectOp):
		return reject(rejectMsg)
	case err != nil:
		return domain.SyncResult{}, err
	default:
		return domain.SyncResult{OpID: op.OpID, Status: result.Status, Message: result.Message}, nil
	}
}

// applySyncOperation entity'ye gore payload'i isler. tx, pushOne'in actigi transaction'dir (processed_request
// rezervasyonu dahil); burada donen hata ValidationError/ConflictError ise transaction tumuyle geri alinir.
func (s *Store) applySyncOperation(ctx context.Context, tx *sql.Tx, deviceID string, op domain.SyncOperation) (*pushOperationResult, error) {
	switch op.Entity {
	case "sales_order":
		if op.Action != "create" {
			return nil, domain.Invalid("sales_order icin yalniz 'create' destekleniyor")
		}
		var req domain.CreateSalesOrderRequest
		if err := strictUnmarshal(op.Payload, &req); err != nil {
			return nil, domain.Invalid("sales_order payload: %s", err)
		}
		// NOT: req.DeviceID push zarfindaki device_id ile OTOMATIK doldurulmaz -- device_id opsiyonel
		// bir alandir ve sales_order.device_id FK'si gercek bir device satirina isaret etmelidir;
		// cihaz kayit uc noktasi henuz yok (bkz. docs/HANDOFF.md TODO), bu yuzden kayitsiz zarf
		// device_id'si burada kullanilirsa FK ihlaliyle reddedilir. Payload kendi device_id'sini
		// vermisse (ve o cihaz kayitliysa) kullanilir; vermemisse NULL kalir.
		if err := req.Validate(); err != nil {
			return nil, err
		}
		order, violations, err := s.createSalesOrderInTx(ctx, tx, req, SaleOptions{Online: false})
		if err != nil {
			return nil, err
		}
		if len(violations) == 0 {
			return &pushOperationResult{Status: domain.SyncApplied, Message: "satis kaydedildi: " + order.OrderNo}, nil
		}
		return &pushOperationResult{Status: domain.SyncFlagged, Message: strings.Join(violations, ",")}, nil

	case "customer_return":
		if op.Action != "create" {
			return nil, domain.Invalid("customer_return icin yalniz 'create' destekleniyor")
		}
		var req domain.CreateCustomerReturnRequest
		if err := strictUnmarshal(op.Payload, &req); err != nil {
			return nil, domain.Invalid("customer_return payload: %s", err)
		}
		if err := req.Validate(); err != nil {
			return nil, err
		}
		ret, err := s.createCustomerReturnInTx(ctx, tx, req)
		if err != nil {
			return nil, err
		}
		return &pushOperationResult{Status: domain.SyncApplied, Message: "iade kaydedildi: " + ret.ReturnNo}, nil

	default:
		return nil, domain.Invalid("desteklenmeyen entity: %s", op.Entity)
	}
}

// strictUnmarshal bilinmeyen alanlari reddeder (REST decodeBody ile ayni kural).
func strictUnmarshal(raw json.RawMessage, v any) error {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// Pull since_seq'ten sonraki degisiklikleri (item, customer, warehouse, lot) seq sirasiyla doner.
// limit varsayilan 500, en fazla 1000. Kalan degisiklik varsa has_more=true.
func (s *Store) Pull(ctx context.Context, sinceSeq int64, limit int, deviceID string) (*domain.SyncPullResponse, error) {
	if limit <= 0 {
		limit = 500
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT seq, entity, entity_id, action, payload_json FROM change_log WHERE seq > ? ORDER BY seq LIMIT ?`,
		sinceSeq, limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	resp := &domain.SyncPullResponse{Changes: []domain.Change{}, NextSeq: sinceSeq, BlockedLotIDs: []string{}}
	for rows.Next() {
		var c domain.Change
		var payload []byte
		if err := rows.Scan(&c.Seq, &c.Entity, &c.EntityID, &c.Action, &payload); err != nil {
			return nil, err
		}
		if len(resp.Changes) == limit {
			resp.HasMore = true
			break
		}
		c.Payload = json.RawMessage(payload)
		resp.Changes = append(resp.Changes, c)
		resp.NextSeq = c.Seq
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	// Bloke/geri cagrilmis lotlar her cekmede tam liste olarak doner: cihaz satis aninda kontrol eder.
	lotRows, err := s.db.QueryContext(ctx, `SELECT id FROM lot WHERE status IN ('blocked', 'recalled') ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer lotRows.Close()
	for lotRows.Next() {
		var id string
		if err := lotRows.Scan(&id); err != nil {
			return nil, err
		}
		resp.BlockedLotIDs = append(resp.BlockedLotIDs, id)
	}
	if err := lotRows.Err(); err != nil {
		return nil, err
	}

	if deviceID != "" {
		if _, err := s.db.ExecContext(ctx, `UPDATE device SET last_sync_at = CURRENT_TIMESTAMP(3) WHERE id = ?`, deviceID); err != nil {
			return nil, err
		}
	}
	return resp, nil
}
