package domain

import (
	"encoding/json"
	"time"
)

// Senkron sozlesmesi (docs/api-contract.md, "Sync"). Android (Room) ve sunucu arasinda:
//   - push: cihaz offline urettigi islemleri gonderir; her islem op_id (UUIDv7) ile idempotenttir.
//   - pull: cihaz since_seq'ten sonraki degisiklikleri ceker.

const (
	SyncApplied   = "applied"   // islem uygulandi
	SyncDuplicate = "duplicate" // op_id daha once islendi; tekrar uygulanmadi
	SyncFlagged   = "flagged"   // kabul edildi ama insan incelemesi gerekiyor (ornek: geri cagrilmis lot satisi)
	SyncRejected  = "rejected"  // gecersiz; cihaz tekrar gondermemeli

	MaxSyncBatch = 500
)

type SyncOperation struct {
	OpID            string          `json:"op_id"`
	Entity          string          `json:"entity"` // ornek: sales_order
	Action          string          `json:"action"` // create | update | delete
	Payload         json.RawMessage `json:"payload"`
	ClientCreatedAt *time.Time      `json:"client_created_at"`
}

type SyncPushRequest struct {
	DeviceID   string          `json:"device_id"`
	Operations []SyncOperation `json:"operations"`
}

type SyncResult struct {
	OpID    string `json:"op_id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type SyncPushResponse struct {
	Results   []SyncResult `json:"results"`
	ServerSeq int64        `json:"server_seq"`
}

type Change struct {
	Seq      int64           `json:"seq"`
	Entity   string          `json:"entity"`
	EntityID string          `json:"entity_id"`
	Action   string          `json:"action"` // upsert | delete
	Payload  json.RawMessage `json:"payload"`
}

type SyncPullResponse struct {
	Changes       []Change `json:"changes"`
	NextSeq       int64    `json:"next_seq"` // sonraki cekmede since_seq olarak gonderilir
	HasMore       bool     `json:"has_more"` // true ise hemen tekrar cekin
	BlockedLotIDs []string `json:"blocked_lot_ids"`
}

func (r *SyncPushRequest) Validate() error {
	if _, err := ParseID(r.DeviceID); err != nil {
		return Invalid("device_id: %s", err)
	}
	if len(r.Operations) == 0 {
		return Invalid("operations bos olamaz")
	}
	if len(r.Operations) > MaxSyncBatch {
		return Invalid("tek istekte en fazla %d islem gonderilebilir", MaxSyncBatch)
	}
	return nil
}
