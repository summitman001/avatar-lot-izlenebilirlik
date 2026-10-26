package com.avatar.lotsales.data

import androidx.room.withTransaction
import com.avatar.lotsales.core.Decimals
import com.avatar.lotsales.core.Uuid7
import com.avatar.lotsales.data.db.AppDatabase
import com.avatar.lotsales.data.db.OutboxEntity
import com.avatar.lotsales.data.db.OutboxStatus
import com.avatar.lotsales.data.db.SalesLineEntity
import com.avatar.lotsales.data.db.SalesOrderEntity
import com.avatar.lotsales.data.db.StockMovementEntity
import com.avatar.lotsales.data.net.AppJson
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
 * Satis onayi = TEK Room transaction'i: sales_orders + sales_lines + her satir icin local_stock_movements
 * (sale_out, negatif) + sync_outbox. Ag cagrisi yoktur; gonderimi SyncWorker yapar (outbox deseni).
 */
class SalesRepository(private val db: AppDatabase, private val newId: () -> String) {
    @Inject constructor(db: AppDatabase) : this(db, Uuid7::generate)

    /** @return olusan siparisin order_no'su. Kural ihlalinde SaleException; hicbir tablo yazilmaz. */
    suspend fun createSale(customerId: String, lines: List<DraftLine>, now: Instant = Instant.now()): String =
        db.withTransaction {
            if (!db.master().customerExists(customerId)) throw SaleException(SaleError.NoCustomer)
            if (lines.isEmpty()) throw SaleException(SaleError.NoLines)

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
            val at = now.truncatedTo(ChronoUnit.MILLIS).toString()
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
                id = orderId, orderNo = orderNo, customerId = customerId, soldAt = at,
                lines = lineRows.map {
                    SalesLinePayload(
                        id = it.id, itemId = it.itemId, lotId = it.lotId, qty = it.qty.toPlainString(),
                        unitPrice = it.unitPrice.toPlainString(), lineTotal = it.lineTotal.toPlainString(),
                        stockMovementId = it.stockMovementId,
                    )
                },
            )

            db.sales().insertOrder(
                SalesOrderEntity(orderId, orderNo, customerId, at, status = "confirmed", totalAmount = total, synced = false),
            )
            db.sales().insertLines(lineRows)
            db.sales().insertMovements(movements)
            db.sync().insertOutbox(
                OutboxEntity(
                    opId = newId(), entity = "sales_order", action = "create",
                    payloadJson = AppJson.encodeToString(SalesOrderPayload.serializer(), payload),
                    createdAt = at, attemptCount = 0, lastError = null, status = OutboxStatus.PENDING,
                ),
            )
            orderNo
        }
}
