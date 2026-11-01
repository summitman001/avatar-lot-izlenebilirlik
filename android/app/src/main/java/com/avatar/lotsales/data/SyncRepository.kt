package com.avatar.lotsales.data

import androidx.room.withTransaction
import com.avatar.lotsales.core.Decimals
import com.avatar.lotsales.core.Uuid7
import com.avatar.lotsales.data.db.AppDatabase
import com.avatar.lotsales.data.db.BlockedLotEntity
import com.avatar.lotsales.data.db.CustomerEntity
import com.avatar.lotsales.data.db.CustomerReturnEntity
import com.avatar.lotsales.data.db.ItemEntity
import com.avatar.lotsales.data.db.LotEntity
import com.avatar.lotsales.data.db.OrderStatus
import com.avatar.lotsales.data.db.OutboxEntity
import com.avatar.lotsales.data.db.OutboxStatus
import com.avatar.lotsales.data.db.SyncKeys
import com.avatar.lotsales.data.db.SyncStateEntity
import com.avatar.lotsales.data.db.WarehouseEntity
import com.avatar.lotsales.data.net.AppJson
import com.avatar.lotsales.data.net.Change
import com.avatar.lotsales.data.net.CustomerDto
import com.avatar.lotsales.data.net.CustomerReturnDto
import com.avatar.lotsales.data.net.ItemDto
import com.avatar.lotsales.data.net.LotDto
import com.avatar.lotsales.data.net.PushOperation
import com.avatar.lotsales.data.net.PushRequest
import com.avatar.lotsales.data.net.SalesOrderReviewDto
import com.avatar.lotsales.data.net.SyncApi
import com.avatar.lotsales.data.net.WarehouseDto
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
    val warehouseId: String? = null,
    val problems: List<OutboxEntity> = emptyList(),
)

class WarehouseChangeBlocked : Exception("Bekleyen işlem varken cihaz deposu değiştirilemez; önce senkronlayın")

/** Senkronda incelemesi sonuclanan satis (status: reviewed | returned); bildirim icin. */
data class SaleReviewUpdate(val orderId: String, val orderNo: String, val status: String)

/**
 * Senkron sirasi (docs/sync.md, docs/android-integration.md): PUSH (outbox sirayla, 500'luk) -> PULL
 * (since_seq, has_more bitene kadar). Lot bakiyesi pull'daki lot payload'inin cihaz deposu kirilimidir.
 * Sunucu hakemdir; Room tek dogruluk kaynagidir, UI agi hic gormez.
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
        SyncStatus(
            pending, kv[SyncKeys.LAST_SYNC_AT], kv[SyncKeys.LAST_ERROR]?.takeIf { it.isNotEmpty() },
            kv[SyncKeys.DEVICE_WAREHOUSE_ID], problems,
        )
    }

    suspend fun sync(): List<SaleReviewUpdate> = mutex.withLock {
        try {
            val deviceId = deviceId()
            push(deviceId)
            val reviews = pull(deviceId)
            putState(SyncKeys.LAST_SYNC_AT, now())
            putState(SyncKeys.LAST_ERROR, "")
            reviews
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            putState(SyncKeys.LAST_ERROR, "${now()} ${e::class.simpleName}: ${e.message.orEmpty()}")
            throw e
        }
    }

    suspend fun deviceId(): String =
        db.sync().state(SyncKeys.DEVICE_ID) ?: Uuid7.generate().also { putState(SyncKeys.DEVICE_ID, it) }

    /**
     * Cihazin satis yaptigi depo. Bakiye depoya gore tutuldugu icin degisince bakiyeler sifirlanir ve
     * imlec 0'a cekilir: sonraki senkron tum lotlari yeni deponun bakiyesiyle yeniden yazar.
     */
    suspend fun setDeviceWarehouse(warehouseId: String) = db.withTransaction {
        if (db.sync().state(SyncKeys.DEVICE_WAREHOUSE_ID) == warehouseId) return@withTransaction
        if (db.sync().pendingCount() > 0) throw WarehouseChangeBlocked()
        putState(SyncKeys.DEVICE_WAREHOUSE_ID, warehouseId)
        putState(SyncKeys.LAST_PULL_SEQ, "0")
        db.master().resetAllLotQty(Decimals.ZERO_QTY)
    }

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
                    applyResult(op, r.status)
                }
            }
            if (batch.size < PUSH_BATCH) return
        }
    }

    /** applied/duplicate/flagged = sunucuda kayitli; rejected = hic yazilmadi, outbox'ta kalir. */
    private suspend fun applyResult(op: OutboxEntity, status: String) {
        val id = AppJson.decodeFromString(PayloadRef.serializer(), op.payloadJson).id
        when (op.entity) {
            "sales_order" -> when (status) {
                OutboxStatus.APPLIED, OutboxStatus.DUPLICATE -> db.sales().markOrderSynced(id, OrderStatus.CONFIRMED)
                OutboxStatus.FLAGGED -> db.sales().markOrderSynced(id, OrderStatus.FLAGGED)
                OutboxStatus.REJECTED -> db.sales().setOrderStatus(id, OrderStatus.REJECTED)
            }
            "customer_return" -> if (status in REACHED_SERVER) db.sales().markReturnSynced(id)
        }
    }

    /**
     * Pull ile gelen lot bakiyesi sunucuya ulasmis kayitlari zaten icerir; onlarin yerel hareketleri ANCAK
     * pull basarili olunca yerel toplamdan cikarilir (synced=1). Aksi halde stok iki kez dusulur: hata hep
     * "eksik goster" yonundedir, fazla satis olmaz. Liste pull'dan ONCE alinir.
     */
    internal suspend fun pull(deviceId: String): List<SaleReviewUpdate> {
        val reachedServer = db.sales().reachedServerRefIds()
        val warehouseId = db.sync().state(SyncKeys.DEVICE_WAREHOUSE_ID)
        var since = db.sync().state(SyncKeys.LAST_PULL_SEQ)?.toLong() ?: 0L
        val statusBefore = mutableMapOf<String, String>() // satis id -> bu senkrondan onceki durum
        do {
            val page = api.pull(since, deviceId, PULL_LIMIT)
            db.withTransaction {
                val reasons = mutableMapOf<String, String?>()
                page.changes.forEach { applyChange(it, warehouseId, reasons, statusBefore) }
                // blocked_lot_ids her cekmede TAM liste: listede olmayanlar kaldirilir
                db.master().deleteBlockedNotIn(page.blockedLotIds)
                db.master().insertBlocked(page.blockedLotIds.map { BlockedLotEntity(it, reasons[it], now()) })
                putState(SyncKeys.LAST_PULL_SEQ, page.nextSeq.toString()) // veri ile imlec ayni transaction'da
            }
            since = page.nextSeq
        } while (page.hasMore)
        if (reachedServer.isNotEmpty()) db.sales().markMovementsSynced(reachedServer)
        // Imlec sifirlanip eski degisiklikler tekrar gelse de yalniz son durumu degisen satis bildirilir.
        return statusBefore.mapNotNull { (id, before) ->
            db.sales().order(id)?.takeIf { it.status != before && it.status in NOTIFY_STATUSES }
                ?.let { SaleReviewUpdate(it.id, it.orderNo, it.status) }
        }
    }

    private suspend fun applyChange(
        change: Change,
        warehouseId: String?,
        blockedReasons: MutableMap<String, String?>,
        statusBefore: MutableMap<String, String>,
    ) {
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
            "warehouse" -> if (delete) db.master().deleteWarehouse(change.entityId) else {
                val d = AppJson.decodeFromJsonElement(WarehouseDto.serializer(), change.payload)
                db.master().upsertWarehouse(WarehouseEntity(d.id, d.code, d.name, d.warehouseType))
            }
            "lot" -> if (delete) db.master().deleteLot(change.entityId) else {
                val d = AppJson.decodeFromJsonElement(LotDto.serializer(), change.payload)
                blockedReasons[d.id] = d.blockedReason
                val cost = BigDecimal(d.unitCost)
                if (db.master().updateLotMeta(d.id, d.lotNo, d.itemId, d.status, d.expiryDate, cost, change.seq) == 0) {
                    db.master().insertLot(LotEntity(d.id, d.lotNo, d.itemId, d.status, d.expiryDate, cost, Decimals.ZERO_QTY, change.seq))
                }
                // Bakiyesiz kayit (eski sunucu) mevcut degeri korur; bakiyeli kayitta yalniz cihaz deposu sayilir.
                d.balances?.let { balances ->
                    val qty = balances.firstOrNull { it.warehouseId == warehouseId }?.qtyOnHand?.let(::BigDecimal) ?: Decimals.ZERO_QTY
                    db.master().updateLotQty(d.id, qty)
                }
            }
            "sales_order" -> if (!delete) {
                val d = AppJson.decodeFromJsonElement(SalesOrderReviewDto.serializer(), change.payload)
                val status = REVIEW_STATUS[d.reviewStatus] ?: return
                val order = db.sales().order(d.id) ?: return // baska cihazin satisi
                if (order.status == OrderStatus.REJECTED) return
                statusBefore.putIfAbsent(d.id, order.status)
                db.sales().setOrderStatus(d.id, status)
            }
            "customer_return" -> if (!delete) {
                val d = AppJson.decodeFromJsonElement(CustomerReturnDto.serializer(), change.payload)
                val lineId = d.salesLineId ?: return
                if (db.sales().line(lineId) == null) return // baska cihazin satisi
                // Hareket yazilmaz: lot upsert'indeki sunucu bakiyesi bu iadeyi zaten icerir.
                db.sales().insertReturnIfAbsent(
                    CustomerReturnEntity(d.id, d.customerId, lineId, d.lotId, BigDecimal(d.qty), d.reason, d.returnedAt, synced = true),
                )
            }
        }
    }

    private suspend fun putState(key: String, value: String) = db.sync().putState(SyncStateEntity(key, value))

    private fun now() = Instant.now().truncatedTo(ChronoUnit.MILLIS).toString()

    private companion object {
        const val PUSH_BATCH = 500
        const val PULL_LIMIT = 500
        val REACHED_SERVER = setOf(OutboxStatus.APPLIED, OutboxStatus.DUPLICATE, OutboxStatus.FLAGGED)

        /** Sunucunun review_status degeri -> yerel satis durumu. */
        val REVIEW_STATUS = mapOf("open" to OrderStatus.FLAGGED, "reviewed" to OrderStatus.REVIEWED, "returned" to OrderStatus.RETURNED)
        val NOTIFY_STATUSES = setOf(OrderStatus.REVIEWED, OrderStatus.RETURNED)
    }
}
