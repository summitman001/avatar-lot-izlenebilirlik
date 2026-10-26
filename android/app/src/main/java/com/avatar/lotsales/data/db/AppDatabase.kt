package com.avatar.lotsales.data.db

import androidx.room.Dao
import androidx.room.Database
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.Query
import androidx.room.RoomDatabase
import androidx.room.TypeConverters
import androidx.room.Upsert
import kotlinx.coroutines.flow.Flow
import java.math.BigDecimal

@Database(
    entities = [
        ItemEntity::class, CustomerEntity::class, LotEntity::class, StockMovementEntity::class,
        SalesOrderEntity::class, SalesLineEntity::class, BlockedLotEntity::class, OutboxEntity::class,
        SyncStateEntity::class,
    ],
    version = 1,
    exportSchema = true,
)
@TypeConverters(Converters::class)
abstract class AppDatabase : RoomDatabase() {
    abstract fun master(): MasterDao
    abstract fun sales(): SalesDao
    abstract fun sync(): SyncDao
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

    /** Lot meta verisini gunceller; qty_on_hand_server'a dokunmaz (bakiye pull'da gelmez). */
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

    @Query("SELECT * FROM lots")
    fun observeLots(): Flow<List<LotEntity>>

    @Query("SELECT * FROM blocked_lots")
    fun observeBlocked(): Flow<List<BlockedLotEntity>>
}

@Dao
interface SalesDao {
    @Insert
    suspend fun insertOrder(order: SalesOrderEntity)

    @Insert
    suspend fun insertLines(lines: List<SalesLineEntity>)

    @Insert
    suspend fun insertMovements(movements: List<StockMovementEntity>)

    @Query("SELECT * FROM local_stock_movements WHERE synced = 0")
    fun observeUnsyncedMovements(): Flow<List<StockMovementEntity>>

    @Query("SELECT * FROM local_stock_movements WHERE synced = 0 AND lot_id = :lotId")
    suspend fun unsyncedMovements(lotId: String): List<StockMovementEntity>

    @Query("UPDATE sales_orders SET synced = 1 WHERE id = :id")
    suspend fun markOrderSynced(id: String)

    /** Sunucuya ulasmis (synced) ama hareketi hala yerel bakiyede sayilan siparisler. */
    @Query(
        """SELECT DISTINCT m.ref_id FROM local_stock_movements m JOIN sales_orders o ON o.id = m.ref_id
           WHERE m.synced = 0 AND m.ref_type = 'sales_order' AND o.synced = 1""",
    )
    suspend fun syncedOrderIdsWithLocalMovements(): List<String>

    @Query("UPDATE local_stock_movements SET synced = 1 WHERE ref_type = 'sales_order' AND ref_id IN (:orderIds)")
    suspend fun markMovementsSynced(orderIds: List<String>)
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
