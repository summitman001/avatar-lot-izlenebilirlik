package com.avatar.lotsales.data

import androidx.room.withTransaction
import com.avatar.lotsales.core.Decimals
import com.avatar.lotsales.core.Uuid7
import com.avatar.lotsales.data.db.AppDatabase
import com.avatar.lotsales.data.db.BlockedLotEntity
import com.avatar.lotsales.data.db.CustomerEntity
import com.avatar.lotsales.data.db.ItemEntity
import com.avatar.lotsales.data.db.LotEntity
import com.avatar.lotsales.data.db.OutboxEntity
import com.avatar.lotsales.data.db.OutboxStatus
import com.avatar.lotsales.data.db.SyncKeys
import com.avatar.lotsales.data.db.SyncStateEntity
import com.avatar.lotsales.data.net.AppJson
import com.avatar.lotsales.data.net.Change
import com.avatar.lotsales.data.net.CustomerDto
import com.avatar.lotsales.data.net.ItemDto
import com.avatar.lotsales.data.net.LotDto
import com.avatar.lotsales.data.net.PushOperation
import com.avatar.lotsales.data.net.PushRequest
import com.avatar.lotsales.data.net.SyncApi
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import java.math.BigDecimal
import java.time.Instant
import java.time.temporal.ChronoUnit
import javax.inject.Inject
import javax.inject.Singleton

data class SyncStatus(
    val pending: Int = 0,
    val lastSyncAt: String? = null,
    val lastError: String? = null,
    val problems: List<OutboxEntity> = emptyList(),
)

/**
 * Senkron sirasi (docs/sync.md): PUSH (outbox sirayla, 500'luk) -> PULL (since_seq, has_more bitene kadar)
 * -> bakiye yenileme (GET /lots). Sunucu hakemdir; Room tek dogruluk kaynagidir, UI agi hic gormez.
 */
@Singleton
class SyncRepository @Inject constructor(private val db: AppDatabase, private val api: SyncApi) {
    private val mutex = Mutex() // periyodik ve tek seferlik is ayni anda calisirsa sirala

    fun observeStatus(): Flow<SyncStatus> = combine(
        db.sync().observePendingCount(),
        db.sync().observeState(),
        db.sync().observeProblems(),
    ) { pending, state, problems ->
        val kv = state.associate { it.key to it.value }
        SyncStatus(pending, kv[SyncKeys.LAST_SYNC_AT], kv[SyncKeys.LAST_ERROR]?.takeIf { it.isNotEmpty() }, problems)
    }

    suspend fun sync() = mutex.withLock {
        try {
            val deviceId = deviceId()
            push(deviceId)
            pull(deviceId)
            refreshBalances()
            putState(SyncKeys.LAST_SYNC_AT, now())
            putState(SyncKeys.LAST_ERROR, "")
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            putState(SyncKeys.LAST_ERROR, "${now()} ${e::class.simpleName}: ${e.message.orEmpty()}")
            throw e
        }
    }

    suspend fun deviceId(): String =
        db.sync().state(SyncKeys.DEVICE_ID) ?: Uuid7.generate().also { putState(SyncKeys.DEVICE_ID, it) }

    internal suspend fun push(deviceId: String) {
        while (true) {
            val batch = db.sync().outboxToSend(PUSH_BATCH)
            if (batch.isEmpty()) return
            val opIds = batch.map { it.opId }
            db.sync().markSent(opIds)
            val response = try {
                api.push(
                    PushRequest(
                        deviceId,
                        batch.map { PushOperation(it.opId, it.entity, it.action, AppJson.parseToJsonElement(it.payloadJson), it.createdAt) },
                    ),
                )
            } catch (e: Exception) {
                if (e !is CancellationException) db.sync().setError(opIds, e.message ?: e::class.simpleName.orEmpty())
                throw e // op'lar 'sent' kalir, sonraki turda ayni op_id ile tekrar gider
            }
            db.withTransaction {
                for (r in response.results) {
                    val op = db.sync().outbox(r.opId) ?: continue
                    db.sync().setResult(r.opId, r.status, r.message.ifEmpty { null })
                    // duplicate = daha once islendi (basari); flagged = sunucu kaydetti ama inceleme istiyor
                    val reachedServer = r.status in setOf(OutboxStatus.APPLIED, OutboxStatus.DUPLICATE, OutboxStatus.FLAGGED)
                    if (reachedServer && op.entity == "sales_order") {
                        db.sales().markOrderSynced(AppJson.decodeFromString(OrderRef.serializer(), op.payloadJson).id)
                    }
                }
            }
            if (batch.size < PUSH_BATCH) return
        }
    }

    internal suspend fun pull(deviceId: String) {
        var since = db.sync().state(SyncKeys.LAST_PULL_SEQ)?.toLong() ?: 0L
        do {
            val page = api.pull(since, deviceId, PULL_LIMIT)
            db.withTransaction {
                val reasons = mutableMapOf<String, String?>()
                page.changes.forEach { applyChange(it, reasons) }
                // blocked_lot_ids her cekmede TAM liste: listede olmayanlar kaldirilir
                db.master().deleteBlockedNotIn(page.blockedLotIds)
                db.master().insertBlocked(page.blockedLotIds.map { BlockedLotEntity(it, reasons[it], now()) })
                putState(SyncKeys.LAST_PULL_SEQ, page.nextSeq.toString()) // veri ile imlec ayni transaction'da
            }
            since = page.nextSeq
        } while (page.hasMore)
    }

    private suspend fun applyChange(change: Change, blockedReasons: MutableMap<String, String?>) {
        val delete = change.action == "delete"
        when (change.entity) {
            "item" -> if (delete) db.master().deleteItem(change.entityId) else {
                val d = AppJson.decodeFromJsonElement(ItemDto.serializer(), change.payload)
                db.master().upsertItem(
                    ItemEntity(d.id, d.code, d.name, d.itemType, d.baseUom, d.unitWeightKg?.let(::BigDecimal), d.defaultSalePrice?.let(::BigDecimal)),
                )
            }
            "customer" -> if (delete) db.master().deleteCustomer(change.entityId) else {
                val d = AppJson.decodeFromJsonElement(CustomerDto.serializer(), change.payload)
                db.master().upsertCustomer(CustomerEntity(d.id, d.code, d.name, d.customerType, d.contactName, d.phone))
            }
            "lot" -> if (delete) db.master().deleteLot(change.entityId) else {
                val d = AppJson.decodeFromJsonElement(LotDto.serializer(), change.payload)
                blockedReasons[d.id] = d.blockedReason
                val cost = BigDecimal(d.unitCost)
                if (db.master().updateLotMeta(d.id, d.lotNo, d.itemId, d.status, d.expiryDate, cost, change.seq) == 0) {
                    // Bakiye bilinmeden satilamaz: refreshBalances gercek degeri yazana kadar 0.
                    db.master().insertLot(LotEntity(d.id, d.lotNo, d.itemId, d.status, d.expiryDate, cost, Decimals.ZERO_QTY, change.seq))
                }
            }
            else -> Unit // warehouse: bugun Room'da tablosu yok
        }
    }

    /**
     * qty_on_hand_server'i sunucudan yeniler. Sunucuya ulasmis siparislerin hareketleri ANCAK bu yenileme
     * basarili olunca yerel toplamdan cikarilir (synced=1); aksi halde stok iki kez dusulur, yani hata
     * hep "eksik goster" yonundedir, fazla satis olmaz. Yenilemeden ONCE alinan liste kullanilir:
     * sunucu bakiyesi bu siparisleri zaten icerir.
     */
    internal suspend fun refreshBalances() {
        val reachedServer = db.sales().syncedOrderIdsWithLocalMovements()
        var offset = 0
        while (true) {
            val items = api.lots(LOTS_PAGE, offset).items
            db.withTransaction {
                items.forEach { lot -> lot.qtyOnHand?.let { db.master().updateLotQty(lot.id, BigDecimal(it)) } }
            }
            if (items.size < LOTS_PAGE) break
            offset += items.size
        }
        if (reachedServer.isNotEmpty()) db.sales().markMovementsSynced(reachedServer)
    }

    private suspend fun putState(key: String, value: String) = db.sync().putState(SyncStateEntity(key, value))

    private fun now() = Instant.now().truncatedTo(ChronoUnit.MILLIS).toString()

    @kotlinx.serialization.Serializable
    private data class OrderRef(val id: String)

    private companion object {
        const val PUSH_BATCH = 500
        const val PULL_LIMIT = 500
        const val LOTS_PAGE = 1000
    }
}
