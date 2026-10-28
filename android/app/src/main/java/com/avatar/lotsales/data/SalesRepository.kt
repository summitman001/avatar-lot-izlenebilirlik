package com.avatar.lotsales.data

import androidx.room.withTransaction
import com.avatar.lotsales.core.Decimals
import com.avatar.lotsales.core.Uuid7
import com.avatar.lotsales.data.db.AppDatabase
import com.avatar.lotsales.data.db.CustomerReturnEntity
import com.avatar.lotsales.data.db.OrderStatus
import com.avatar.lotsales.data.db.OutboxEntity
import com.avatar.lotsales.data.db.OutboxStatus
import com.avatar.lotsales.data.db.SalesLineEntity
import com.avatar.lotsales.data.db.SalesOrderEntity
import com.avatar.lotsales.data.db.StockMovementEntity
import com.avatar.lotsales.data.db.SyncKeys
import com.avatar.lotsales.data.net.AppJson
import com.avatar.lotsales.data.net.CustomerReturnPayload
import com.avatar.lotsales.data.net.SalesLinePayload
import com.avatar.lotsales.data.net.SalesOrderPayload
import com.avatar.lotsales.domain.SaleError
import com.avatar.lotsales.domain.SaleException
import com.avatar.lotsales.domain.SaleValidator
import java.math.BigDecimal
import java.time.Instant
import java.time.temporal.ChronoUnit
import javax.inject.Inject

data class DraftLine(val itemId: String, val lotId: String, val qty: BigDecimal, val unitPrice: BigDecimal)

/**
 * Yazmalar = TEK Room transaction'i, ag cagrisi yok (outbox deseni; gonderimi SyncWorker yapar):
 *  - satis: sales_orders + sales_lines + her satir icin sale_out hareketi (-) + sync_outbox
 *  - iade: customer_returns + return_in hareketi (+) + sync_outbox
 */
class SalesRepository(private val db: AppDatabase, private val newId: () -> String) {
    @Inject constructor(db: AppDatabase) : this(db, Uuid7::generate)

    /** @return olusan siparisin order_no'su. Kural ihlalinde SaleException; hicbir tablo yazilmaz. */
    suspend fun createSale(customerId: String, lines: List<DraftLine>, now: Instant = Instant.now()): String =
        db.withTransaction {
            if (!db.master().customerExists(customerId)) throw SaleException(SaleError.NoCustomer)
            if (lines.isEmpty()) throw SaleException(SaleError.NoLines)
            val warehouseId = db.sync().state(SyncKeys.DEVICE_WAREHOUSE_ID) ?: throw SaleException(SaleError.NoWarehouse)

            // Ekrandaki kontrol yetmez: arada pull lotu bloke etmis veya baska satis stogu dusurmus olabilir.
            val reserved = mutableMapOf<String, BigDecimal>()
            for (line in lines) {
                val lot = db.master().lot(line.lotId) ?: throw SaleException(SaleError.UnknownLot)
                val available = db.sales().unsyncedMovements(lot.id)
                    .fold(lot.qtyOnHandServer) { acc, m -> acc + m.qty }
                val sellable = lot.status == "active" && !db.master().isBlocked(lot.id)
                val already = reserved[lot.id] ?: BigDecimal.ZERO
                SaleValidator.validateLine(lot.lotNo, sellable, available, already, line.qty, line.unitPrice)
                    ?.let { throw SaleException(it) }
                reserved[lot.id] = already + line.qty
            }

            val orderId = newId()
            val orderNo = "S-" + orderId.takeLast(10).uppercase() // UUIDv7'nin rastgele kismi: cihazlar arasi cakismaz
            val at = iso(now)
            val lineRows = lines.map { l ->
                SalesLineEntity(
                    id = newId(), salesOrderId = orderId, itemId = l.itemId, lotId = l.lotId,
                    qty = l.qty.setScale(Decimals.QTY_SCALE),
                    unitPrice = l.unitPrice.setScale(Decimals.PRICE_SCALE),
                    lineTotal = Decimals.lineTotal(l.qty, l.unitPrice),
                    stockMovementId = newId(),
                )
            }
            val movements = lineRows.map { l ->
                StockMovementEntity(
                    id = l.stockMovementId, lotId = l.lotId, movementType = "sale_out", qty = l.qty.negate(),
                    refType = "sales_order", refId = orderId, createdAt = at, synced = false,
                )
            }
            val total = lineRows.fold(BigDecimal.ZERO.setScale(Decimals.MONEY_SCALE)) { acc, l -> acc + l.lineTotal }
            val payload = SalesOrderPayload(
                id = orderId, orderNo = orderNo, customerId = customerId, warehouseId = warehouseId, soldAt = at,
                lines = lineRows.map { SalesLinePayload(it.id, it.itemId, it.lotId, it.qty.toPlainString(), it.unitPrice.toPlainString()) },
            )

            db.sales().insertOrder(SalesOrderEntity(orderId, orderNo, customerId, at, OrderStatus.CONFIRMED, total, synced = false))
            db.sales().insertLines(lineRows)
            db.sales().insertMovements(movements)
            enqueue("sales_order", AppJson.encodeToString(SalesOrderPayload.serializer(), payload), at)
            orderNo
        }

    /** Satilan bir satirin iadesi; sunucu satisi bilmeli (outbox sirasi: satis once gider). */
    suspend fun createReturn(salesLineId: String, qty: BigDecimal?, reason: String, now: Instant = Instant.now()) =
        db.withTransaction {
            val line = db.sales().line(salesLineId) ?: throw SaleException(SaleError.UnknownLine)
            val order = db.sales().order(line.salesOrderId) ?: throw SaleException(SaleError.UnknownLine)
            if (order.status == OrderStatus.REJECTED) throw SaleException(SaleError.OrderRejected)
            val returned = db.sales().returnsForLine(line.id).fold(BigDecimal.ZERO) { acc, r -> acc + r.qty }
            SaleValidator.validateReturn(line.qty, returned, qty, reason)?.let { throw SaleException(it) }

            val returnId = newId()
            val at = iso(now)
            val q = qty!!.setScale(Decimals.QTY_SCALE)
            db.sales().insertReturn(CustomerReturnEntity(returnId, order.customerId, line.id, line.lotId, q, reason.trim(), at, synced = false))
            db.sales().insertMovements(
                listOf(StockMovementEntity(newId(), line.lotId, "return_in", q, "customer_return", returnId, at, synced = false)),
            )
            val payload = CustomerReturnPayload(returnId, order.customerId, line.id, line.lotId, q.toPlainString(), reason.trim(), at)
            enqueue("customer_return", AppJson.encodeToString(CustomerReturnPayload.serializer(), payload), at)
        }

    fun observeSales() = db.sales().observeSales()

    fun observeSale(orderId: String) = db.sales().observeSale(orderId)

    fun observeLines(orderId: String) = db.sales().observeSaleLines(orderId)

    fun observeReturns(orderId: String) = db.sales().observeReturnsForOrder(orderId)

    /** flagged: sunucuda kayitli; kullanici uyariyi gordu -> outbox kaydi silinir (android-integration.md). */
    suspend fun acknowledge(opId: String) = db.sync().deleteOutbox(opId)

    /**
     * rejected: sunucu hicbir sey yazmadi. Kullanici vazgecince kayit yerelde "rejected" kalir (iz), stok
     * etkisi geri alinir ve outbox'tan silinir.
     */
    suspend fun discardRejected(opId: String) = db.withTransaction {
        val op = db.sync().outbox(opId) ?: return@withTransaction
        if (op.status != OutboxStatus.REJECTED) return@withTransaction
        val refId = AppJson.decodeFromString(PayloadRef.serializer(), op.payloadJson).id
        if (op.entity == "sales_order") db.sales().setOrderStatus(refId, OrderStatus.REJECTED)
        db.sales().markMovementsSynced(listOf(refId))
        db.sync().deleteOutbox(opId)
    }

    private suspend fun enqueue(entity: String, payloadJson: String, at: String) = db.sync().insertOutbox(
        OutboxEntity(newId(), entity, "create", payloadJson, at, attemptCount = 0, lastError = null, status = OutboxStatus.PENDING),
    )

    private fun iso(now: Instant) = now.truncatedTo(ChronoUnit.MILLIS).toString()
}

/** Outbox payload'indan yalnizca kaydin id'si. */
@kotlinx.serialization.Serializable
internal data class PayloadRef(val id: String)
