package com.avatar.lotsales.data.db

import androidx.room.AutoMigration
import androidx.room.ColumnInfo
import androidx.room.Dao
import androidx.room.Database
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.Query
import androidx.room.RoomDatabase
import androidx.room.TypeConverters
import androidx.room.Upsert
import androidx.room.migration.AutoMigrationSpec
import androidx.sqlite.db.SupportSQLiteDatabase
import kotlinx.coroutines.flow.Flow
import java.math.BigDecimal

@Database(
    entities = [
        ItemEntity::class, CustomerEntity::class, LotEntity::class, StockMovementEntity::class,
        SalesOrderEntity::class, SalesLineEntity::class, BlockedLotEntity::class, OutboxEntity::class,
        SyncStateEntity::class, WarehouseEntity::class, CustomerReturnEntity::class,
    ],
    version = 2,
    exportSchema = true,
    autoMigrations = [AutoMigration(from = 1, to = 2, spec = Migration1To2::class)], // v2: warehouses + customer_returns
)
@TypeConverters(Converters::class)
abstract class AppDatabase : RoomDatabase() {
    abstract fun master(): MasterDao
    abstract fun sales(): SalesDao
    abstract fun sync(): SyncDao
}

/**
 * v1 pull'da warehouse kayitlarini atiyor ve qty_on_hand_server'a TUM depolarin toplamini yaziyordu.
 * Imlec o kayitlari coktan gectigi icin v2'de depolar hic gelmezdi: imlec sifirlanir, bakiyeler 0'lanir;
 * ilk senkron her seyi (depolar + cihaz deposu bakiyeleri) yeniden ceker.
 */
class Migration1To2 : AutoMigrationSpec {
    override fun onPostMigrate(db: SupportSQLiteDatabase) {
        db.execSQL("UPDATE sync_state SET value = '0' WHERE `key` = 'last_pull_seq'")
        db.execSQL("UPDATE lots SET qty_on_hand_server = '0.000'")
    }
}

/** Sunucudan pull ile gelen ana veri ve lotlar (cihaz bunlari olusturmaz). */
@Dao
interface MasterDao {
    @Upsert
    suspend fun upsertItem(item: ItemEntity)

    @Query("DELETE FROM items WHERE id = :id")
    suspend fun deleteItem(id: String)

    @Upsert
    suspend fun upsertCustomer(customer: CustomerEntity)

    @Query("DELETE FROM customers WHERE id = :id")
    suspend fun deleteCustomer(id: String)

    @Upsert
    suspend fun upsertWarehouse(warehouse: WarehouseEntity)

    @Query("DELETE FROM warehouses WHERE id = :id")
    suspend fun deleteWarehouse(id: String)

    /** Lot meta verisini gunceller; qty_on_hand_server'a dokunmaz (bakiye ayri yazilir). */
    @Query(
        """UPDATE lots SET lot_no = :lotNo, item_id = :itemId, status = :status, expiry_date = :expiryDate,
           unit_cost = :unitCost, updated_seq = :seq WHERE id = :id""",
    )
    suspend fun updateLotMeta(
        id: String, lotNo: String, itemId: String, status: String, expiryDate: String?, unitCost: BigDecimal, seq: Long,
    ): Int

    @Insert(onConflict = OnConflictStrategy.IGNORE)
    suspend fun insertLot(lot: LotEntity)

    @Query("DELETE FROM lots WHERE id = :id")
    suspend fun deleteLot(id: String)

    @Query("UPDATE lots SET qty_on_hand_server = :qty WHERE id = :id")
    suspend fun updateLotQty(id: String, qty: BigDecimal)

    @Query("UPDATE lots SET qty_on_hand_server = :qty")
    suspend fun resetAllLotQty(qty: BigDecimal)

    @Query("DELETE FROM blocked_lots WHERE lot_id NOT IN (:keep)")
    suspend fun deleteBlockedNotIn(keep: List<String>)

    @Insert(onConflict = OnConflictStrategy.IGNORE) // mevcut kaydin reason/received_at'i korunur
    suspend fun insertBlocked(rows: List<BlockedLotEntity>)

    @Query("SELECT * FROM lots WHERE id = :id")
    suspend fun lot(id: String): LotEntity?

    @Query("SELECT EXISTS(SELECT 1 FROM blocked_lots WHERE lot_id = :lotId)")
    suspend fun isBlocked(lotId: String): Boolean

    @Query("SELECT EXISTS(SELECT 1 FROM customers WHERE id = :id)")
    suspend fun customerExists(id: String): Boolean

    @Query("SELECT * FROM items ORDER BY name")
    fun observeItems(): Flow<List<ItemEntity>>

    @Query("SELECT * FROM customers ORDER BY name")
    fun observeCustomers(): Flow<List<CustomerEntity>>

    @Query("SELECT * FROM warehouses ORDER BY code")
    fun observeWarehouses(): Flow<List<WarehouseEntity>>

    @Query("SELECT * FROM lots")
    fun observeLots(): Flow<List<LotEntity>>

    @Query("SELECT * FROM blocked_lots")
    fun observeBlocked(): Flow<List<BlockedLotEntity>>
}

data class SaleSummary(
    val id: String,
    @ColumnInfo(name = "order_no") val orderNo: String,
    @ColumnInfo(name = "customer_name") val customerName: String?,
    @ColumnInfo(name = "sold_at") val soldAt: String,
    @ColumnInfo(name = "total_amount") val totalAmount: BigDecimal,
    val status: String,
    val synced: Boolean,
)

data class SaleLineView(
    val id: String,
    @ColumnInfo(name = "item_name") val itemName: String?,
    @ColumnInfo(name = "lot_no") val lotNo: String?,
    @ColumnInfo(name = "lot_id") val lotId: String,
    val qty: BigDecimal,
    @ColumnInfo(name = "unit_price") val unitPrice: BigDecimal,
    @ColumnInfo(name = "line_total") val lineTotal: BigDecimal,
)

@Dao
interface SalesDao {
    @Insert
    suspend fun insertOrder(order: SalesOrderEntity)

    @Insert
    suspend fun insertLines(lines: List<SalesLineEntity>)

    @Insert
    suspend fun insertMovements(movements: List<StockMovementEntity>)

    @Insert
    suspend fun insertReturn(row: CustomerReturnEntity)

    @Query("SELECT * FROM local_stock_movements WHERE synced = 0")
    fun observeUnsyncedMovements(): Flow<List<StockMovementEntity>>

    @Query("SELECT * FROM local_stock_movements WHERE synced = 0 AND lot_id = :lotId")
    suspend fun unsyncedMovements(lotId: String): List<StockMovementEntity>

    @Query("SELECT * FROM sales_orders WHERE id = :id")
    suspend fun order(id: String): SalesOrderEntity?

    @Query("SELECT * FROM sales_lines WHERE id = :id")
    suspend fun line(id: String): SalesLineEntity?

    @Query("SELECT * FROM customer_returns WHERE sales_line_id = :lineId")
    suspend fun returnsForLine(lineId: String): List<CustomerReturnEntity>

    @Query("UPDATE sales_orders SET synced = 1, status = :status WHERE id = :id")
    suspend fun markOrderSynced(id: String, status: String)

    @Query("UPDATE sales_orders SET status = :status WHERE id = :id")
    suspend fun setOrderStatus(id: String, status: String)

    @Query("UPDATE customer_returns SET synced = 1 WHERE id = :id")
    suspend fun markReturnSynced(id: String)

    /** Sunucuya ulasmis (satis/iade) ama hareketi hala yerel bakiyede sayilan kayitlarin id'leri. */
    @Query(
        """SELECT DISTINCT m.ref_id FROM local_stock_movements m
           WHERE m.synced = 0 AND (
             (m.ref_type = 'sales_order' AND m.ref_id IN (SELECT id FROM sales_orders WHERE synced = 1)) OR
             (m.ref_type = 'customer_return' AND m.ref_id IN (SELECT id FROM customer_returns WHERE synced = 1)))""",
    )
    suspend fun reachedServerRefIds(): List<String>

    @Query("UPDATE local_stock_movements SET synced = 1 WHERE ref_id IN (:refIds)")
    suspend fun markMovementsSynced(refIds: List<String>)

    @Query(
        """SELECT o.id, o.order_no, c.name AS customer_name, o.sold_at, o.total_amount, o.status, o.synced
           FROM sales_orders o LEFT JOIN customers c ON c.id = o.customer_id ORDER BY o.sold_at DESC""",
    )
    fun observeSales(): Flow<List<SaleSummary>>

    @Query(
        """SELECT o.id, o.order_no, c.name AS customer_name, o.sold_at, o.total_amount, o.status, o.synced
           FROM sales_orders o LEFT JOIN customers c ON c.id = o.customer_id WHERE o.id = :id""",
    )
    fun observeSale(id: String): Flow<SaleSummary?>

    @Query(
        """SELECT l.id, i.name AS item_name, lt.lot_no, l.lot_id, l.qty, l.unit_price, l.line_total
           FROM sales_lines l LEFT JOIN items i ON i.id = l.item_id LEFT JOIN lots lt ON lt.id = l.lot_id
           WHERE l.sales_order_id = :orderId ORDER BY l.id""",
    )
    fun observeSaleLines(orderId: String): Flow<List<SaleLineView>>

    @Query(
        """SELECT r.* FROM customer_returns r JOIN sales_lines l ON l.id = r.sales_line_id
           WHERE l.sales_order_id = :orderId""",
    )
    fun observeReturnsForOrder(orderId: String): Flow<List<CustomerReturnEntity>>
}

@Dao
interface SyncDao {
    @Insert
    suspend fun insertOutbox(op: OutboxEntity)

    /** 'sent' de tekrar gonderilir: yanit alinamadan kesilen istek op_id sayesinde guvenle tekrarlanir. */
    @Query("SELECT * FROM sync_outbox WHERE status IN ('pending', 'sent') ORDER BY created_at, op_id LIMIT :limit")
    suspend fun outboxToSend(limit: Int): List<OutboxEntity>

    @Query("UPDATE sync_outbox SET status = 'sent', attempt_count = attempt_count + 1 WHERE op_id IN (:opIds)")
    suspend fun markSent(opIds: List<String>)

    @Query("UPDATE sync_outbox SET last_error = :error WHERE op_id IN (:opIds)")
    suspend fun setError(opIds: List<String>, error: String)

    @Query("UPDATE sync_outbox SET status = :status, last_error = :message WHERE op_id = :opId")
    suspend fun setResult(opId: String, status: String, message: String?)

    @Query("SELECT * FROM sync_outbox WHERE op_id = :opId")
    suspend fun outbox(opId: String): OutboxEntity?

    @Query("DELETE FROM sync_outbox WHERE op_id = :opId")
    suspend fun deleteOutbox(opId: String)

    @Query("SELECT COUNT(*) FROM sync_outbox WHERE status IN ('pending', 'sent')")
    suspend fun pendingCount(): Int

    @Query("SELECT COUNT(*) FROM sync_outbox WHERE status IN ('pending', 'sent')")
    fun observePendingCount(): Flow<Int>

    @Query("SELECT * FROM sync_outbox WHERE status IN ('flagged', 'rejected') ORDER BY created_at DESC")
    fun observeProblems(): Flow<List<OutboxEntity>>

    @Query("SELECT value FROM sync_state WHERE `key` = :key")
    suspend fun state(key: String): String?

    @Upsert
    suspend fun putState(state: SyncStateEntity)

    @Query("SELECT * FROM sync_state")
    fun observeState(): Flow<List<SyncStateEntity>>
}
