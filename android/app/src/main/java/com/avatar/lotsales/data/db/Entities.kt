package com.avatar.lotsales.data.db

import androidx.room.ColumnInfo
import androidx.room.Entity
import androidx.room.Index
import androidx.room.PrimaryKey
import androidx.room.TypeConverter
import java.math.BigDecimal

// Para/miktar BigDecimal; Room'da TEXT (toPlainString) saklanir. SQL'de bu kolonlarla aritmetik
// YAPILMAZ (SQLite REAL'e cevirir), toplamlar Kotlin'de BigDecimal ile alinir.
class Converters {
    @TypeConverter
    fun fromDecimal(value: BigDecimal?): String? = value?.toPlainString()

    @TypeConverter
    fun toDecimal(value: String?): BigDecimal? = value?.let(::BigDecimal)
}

@Entity(tableName = "items")
data class ItemEntity(
    @PrimaryKey val id: String,
    val code: String,
    val name: String,
    @ColumnInfo(name = "item_type") val itemType: String,
    @ColumnInfo(name = "base_uom") val baseUom: String,
    @ColumnInfo(name = "unit_weight_kg") val unitWeightKg: BigDecimal?,
    @ColumnInfo(name = "default_sale_price") val defaultSalePrice: BigDecimal?,
)

@Entity(tableName = "customers")
data class CustomerEntity(
    @PrimaryKey val id: String,
    val code: String,
    val name: String,
    @ColumnInfo(name = "customer_type") val customerType: String,
    @ColumnInfo(name = "contact_name") val contactName: String?,
    val phone: String?,
)

@Entity(tableName = "lots", indices = [Index("item_id")])
data class LotEntity(
    @PrimaryKey val id: String,
    @ColumnInfo(name = "lot_no") val lotNo: String,
    @ColumnInfo(name = "item_id") val itemId: String,
    val status: String,
    @ColumnInfo(name = "expiry_date") val expiryDate: String?, // YYYY-MM-DD
    @ColumnInfo(name = "unit_cost") val unitCost: BigDecimal,
    @ColumnInfo(name = "qty_on_hand_server") val qtyOnHandServer: BigDecimal,
    @ColumnInfo(name = "updated_seq") val updatedSeq: Long,
)

/** synced=false olan hareketler yerel kullanilabilir miktara eklenir (bkz. SyncRepository.refreshBalances). */
@Entity(tableName = "local_stock_movements", indices = [Index("lot_id"), Index("ref_id")])
data class StockMovementEntity(
    @PrimaryKey val id: String,
    @ColumnInfo(name = "lot_id") val lotId: String,
    @ColumnInfo(name = "movement_type") val movementType: String,
    val qty: BigDecimal, // isaretli: sale_out negatif
    @ColumnInfo(name = "ref_type") val refType: String,
    @ColumnInfo(name = "ref_id") val refId: String,
    @ColumnInfo(name = "created_at") val createdAt: String,
    val synced: Boolean,
)

@Entity(tableName = "sales_orders", indices = [Index(value = ["order_no"], unique = true)])
data class SalesOrderEntity(
    @PrimaryKey val id: String,
    @ColumnInfo(name = "order_no") val orderNo: String,
    @ColumnInfo(name = "customer_id") val customerId: String,
    @ColumnInfo(name = "sold_at") val soldAt: String,
    val status: String,
    @ColumnInfo(name = "total_amount") val totalAmount: BigDecimal,
    val synced: Boolean,
)

@Entity(tableName = "sales_lines", indices = [Index("sales_order_id"), Index("lot_id")])
data class SalesLineEntity(
    @PrimaryKey val id: String,
    @ColumnInfo(name = "sales_order_id") val salesOrderId: String,
    @ColumnInfo(name = "item_id") val itemId: String,
    @ColumnInfo(name = "lot_id") val lotId: String,
    val qty: BigDecimal,
    @ColumnInfo(name = "unit_price") val unitPrice: BigDecimal,
    @ColumnInfo(name = "line_total") val lineTotal: BigDecimal,
    @ColumnInfo(name = "stock_movement_id") val stockMovementId: String,
)

@Entity(tableName = "blocked_lots")
data class BlockedLotEntity(
    @PrimaryKey @ColumnInfo(name = "lot_id") val lotId: String,
    val reason: String?,
    @ColumnInfo(name = "received_at") val receivedAt: String,
)

object OutboxStatus {
    const val PENDING = "pending"
    const val SENT = "sent"
    const val APPLIED = "applied"
    const val DUPLICATE = "duplicate"
    const val FLAGGED = "flagged"
    const val REJECTED = "rejected"
}

@Entity(tableName = "sync_outbox", indices = [Index("status")])
data class OutboxEntity(
    @PrimaryKey @ColumnInfo(name = "op_id") val opId: String,
    val entity: String,
    val action: String,
    @ColumnInfo(name = "payload_json") val payloadJson: String,
    @ColumnInfo(name = "created_at") val createdAt: String,
    @ColumnInfo(name = "attempt_count") val attemptCount: Int,
    @ColumnInfo(name = "last_error") val lastError: String?,
    val status: String,
)

@Entity(tableName = "sync_state")
data class SyncStateEntity(
    @PrimaryKey val key: String,
    val value: String,
)

object SyncKeys {
    const val LAST_PULL_SEQ = "last_pull_seq"
    const val LAST_SYNC_AT = "last_sync_at"
    const val DEVICE_ID = "device_id"
    const val LAST_ERROR = "last_error"
}
