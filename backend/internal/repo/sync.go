package repo

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/go-sql-driver/mysql"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
)

var syncActions = map[string]bool{"create": true, "update": true, "delete": true}

// StubMessage: payload isleme henuz yazilmadigi icin push sonucunda donen mesaj.
const StubMessage = "TODO: payload isleme henuz uygulanmadi; op_id kaydedildi, islem uygulanmadi"

func (s *Store) ServerSeq(ctx context.Context) (int64, error) {
	var seq int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) FROM change_log`).Scan(&seq)
	return seq, err
}

// Push cihazdan gelen islemleri isler.
//
// BUGUN STUB: op_id processed_request'e yazilir, ayni op_id tekrar gelirse "duplicate" doner.
// Payload isleme (sales_order -> stok hareketi, geri cagirma kontrolu, ...) TODO'dur; bu yuzden ilk
// gelen islem "applied" degil "flagged" doner (istemci "uygulandi" sanip yerel kaydi silmesin).
// Payload isleme yazildiginda stub kayitlari temizleyin (docs/HANDOFF.md):
//
//	DELETE FROM processed_request WHERE JSON_EXTRACT(response_json, '$.stub') = TRUE;
func (s *Store) Push(ctx context.Context, req domain.SyncPushRequest) (*domain.SyncPushResponse, error) {
	resp := &domain.SyncPushResponse{Results: make([]domain.SyncResult, 0, len(req.Operations))}
	for _, op := range req.Operations {
		res, err := s.pushOne(ctx, req.DeviceID, op)
		if err != nil {
			return nil, err
		}
		resp.Results = append(resp.Results, res)
	}
	// Cihazin son senkron zamani (cihaz kayitliysa).
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

	// TODO: burada op.Entity'ye gore payload'i isleyin (ayni transaction'da), sonucu stub yerine yazin.
	status, msg := domain.SyncFlagged, StubMessage
	stored, _ := json.Marshal(map[string]any{"message": msg, "stub": true})

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO processed_request (op_id, device_id, status, response_json) VALUES (?, ?, ?, ?)`,
		op.OpID, deviceID, status, string(stored))
	var me *mysql.MySQLError
	if errors.As(err, &me) && me.Number == errDupEntry {
		var prev string
		if qerr := s.db.QueryRowContext(ctx, `SELECT status FROM processed_request WHERE op_id = ?`, op.OpID).Scan(&prev); qerr != nil {
			return domain.SyncResult{}, qerr
		}
		return domain.SyncResult{OpID: op.OpID, Status: domain.SyncDuplicate,
			Message: "op_id daha once islendi (ilk sonuc: " + prev + ")"}, nil
	}
	if err != nil {
		return domain.SyncResult{}, err
	}
	return domain.SyncResult{OpID: op.OpID, Status: status, Message: msg}, nil
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
