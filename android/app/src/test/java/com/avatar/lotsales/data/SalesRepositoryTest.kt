package com.avatar.lotsales.data

import android.database.sqlite.SQLiteConstraintException
import androidx.test.ext.junit.runners.AndroidJUnit4
import com.avatar.lotsales.core.Uuid7
import com.avatar.lotsales.data.db.AppDatabase
import com.avatar.lotsales.data.db.BlockedLotEntity
import com.avatar.lotsales.data.db.CustomerEntity
import com.avatar.lotsales.data.db.ItemEntity
import com.avatar.lotsales.data.db.LotEntity
import com.avatar.lotsales.data.db.OutboxEntity
import com.avatar.lotsales.data.net.AppJson
import com.avatar.lotsales.data.net.SalesOrderPayload
import com.avatar.lotsales.domain.SaleError
import com.avatar.lotsales.domain.SaleException
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import java.math.BigDecimal

@RunWith(AndroidJUnit4::class)
class SalesRepositoryTest {
    private lateinit var db: AppDatabase

    @Before
    fun setUp() = runBlocking {
        db = inMemoryDb()
        db.master().upsertItem(ItemEntity(ITEM, "SUCUK-NARIN-1KG", "Narin Sucuk 1 kg", "finished", "adet", BigDecimal("1.000"), BigDecimal("24.9000")))
        db.master().upsertCustomer(CustomerEntity(CUSTOMER, "MUS-A", "Market A", "market", null, null))
        db.master().insertLot(LotEntity(LOT, "SUC-2026-045", ITEM, "active", "2026-12-15", BigDecimal("18.1588"), BigDecimal("85.000"), 6))
        db.master().insertLot(LotEntity(BLOCKED_LOT, "SUC-2026-099", ITEM, "active", "2026-11-30", BigDecimal("18.2000"), BigDecimal("40.000"), 8))
        db.master().insertBlocked(listOf(BlockedLotEntity(BLOCKED_LOT, "geri cagirma", "2026-10-26T10:00:00Z")))
    }

    @After
    fun tearDown() = db.close()

    private fun line(qty: String, lot: String = LOT) = DraftLine(ITEM, lot, BigDecimal(qty), BigDecimal("24.9000"))

    @Test
    fun confirmWritesOrderLinesMovementsAndOutboxTogether() = runBlocking {
        val orderNo = SalesRepository(db).createSale(CUSTOMER, listOf(line("2"), line("1")))

        assertEquals(1, db.count("sales_orders"))
        assertEquals(2, db.count("sales_lines"))
        assertEquals(2, db.count("local_stock_movements"))
        assertEquals(1, db.count("sync_outbox"))

        val orderId = db.column("SELECT id FROM sales_orders").single()!!
        assertEquals(listOf(orderNo), db.column("SELECT order_no FROM sales_orders"))
        assertEquals(listOf("74.70"), db.column("SELECT total_amount FROM sales_orders"))
        // her satirin stok hareketi var: sale_out, negatif, siparise bagli
        assertEquals(
            db.column("SELECT stock_movement_id FROM sales_lines ORDER BY stock_movement_id"),
            db.column("SELECT id FROM local_stock_movements ORDER BY id"),
        )
        assertEquals(listOf("-1.000", "-2.000"), db.column("SELECT qty FROM local_stock_movements ORDER BY qty"))
        assertEquals(listOf("sale_out"), db.column("SELECT DISTINCT movement_type FROM local_stock_movements"))
        assertEquals(listOf(orderId), db.column("SELECT DISTINCT ref_id FROM local_stock_movements"))

        // outbox govdesi sozlesmedeki sales_order payload'i; decimal'ler string ve sabit olcekli
        assertEquals(listOf("pending"), db.column("SELECT status FROM sync_outbox"))
        val payload = AppJson.decodeFromString(SalesOrderPayload.serializer(), db.column("SELECT payload_json FROM sync_outbox").single()!!)
        assertEquals(orderId, payload.id)
        assertEquals(listOf("2.000", "1.000"), payload.lines.map { it.qty })
        assertEquals(listOf("49.80", "24.90"), payload.lines.map { it.lineTotal })
        assertEquals("24.9000", payload.lines.first().unitPrice)

        val available = LotRepository(db).observeLots().first().single { it.lot.id == LOT }.available
        assertEquals(BigDecimal("82.000"), available)
    }

    @Test
    fun failureInsideTransactionWritesNothing() = runBlocking {
        // outbox'a ayni op_id onceden yazilmis: son insert (outbox) PK ihlaliyle patlar
        db.sync().insertOutbox(OutboxEntity("dup-op", "x", "create", "{}", "2026-10-26T00:00:00Z", 0, null, "applied"))
        val ids = ArrayDeque(listOf(Uuid7.generate(), Uuid7.generate(), Uuid7.generate(), "dup-op")) // siparis, satir, hareket, op

        val error = runCatching { SalesRepository(db) { ids.removeFirst() }.createSale(CUSTOMER, listOf(line("2"))) }.exceptionOrNull()

        assertTrue("PK ihlali beklenirdi: $error", error is SQLiteConstraintException)
        assertEquals(0, db.count("sales_orders"))
        assertEquals(0, db.count("sales_lines"))
        assertEquals(0, db.count("local_stock_movements"))
        assertEquals(1, db.count("sync_outbox")) // yalnizca onceden var olan
    }

    @Test
    fun blockedLotInsufficientStockAndBadQtyAreRejectedWithoutWrites() = runBlocking {
        val repo = SalesRepository(db)
        suspend fun errorOf(vararg lines: DraftLine) = (runCatching { repo.createSale(CUSTOMER, lines.toList()) }.exceptionOrNull() as SaleException).error

        assertTrue(errorOf(line("1", BLOCKED_LOT)) is SaleError.LotNotSellable)
        assertTrue(errorOf(line("50"), line("36")) is SaleError.InsufficientStock) // ayni lotta toplam 86 > 85
        assertEquals(SaleError.InvalidQty, errorOf(line("0")))
        assertEquals(SaleError.NoLines, errorOf())
        assertEquals(0, db.count("sales_orders") + db.count("sales_lines") + db.count("local_stock_movements") + db.count("sync_outbox"))
    }

    private companion object {
        const val ITEM = "01a17000-0000-7000-8000-000000000001"
        const val CUSTOMER = "01a17000-0000-7000-8000-000000000101"
        const val LOT = "01a17000-0000-7000-8000-000000000201"
        const val BLOCKED_LOT = "01a17000-0000-7000-8000-000000000202"
    }
}
