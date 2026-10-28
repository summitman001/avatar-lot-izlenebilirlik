package com.avatar.lotsales.data

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import com.avatar.lotsales.data.db.AppDatabase
import com.avatar.lotsales.data.net.AppJson
import com.avatar.lotsales.data.net.FakeApiInterceptor
import com.avatar.lotsales.data.net.SyncApi
import com.avatar.lotsales.domain.SaleError
import com.avatar.lotsales.domain.SaleException
import com.avatar.lotsales.domain.violationText
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import retrofit2.Retrofit
import retrofit2.converter.kotlinx.serialization.asConverterFactory
import retrofit2.create
import java.math.BigDecimal

/** Debug assets'teki FakeApi (Go sunucusunun push kurallarini taklit eder) ile uctan uca senkron. */
@RunWith(AndroidJUnit4::class)
class SyncRepositoryTest {
    private lateinit var db: AppDatabase
    private lateinit var sync: SyncRepository
    private lateinit var sales: SalesRepository

    @Before
    fun setUp() {
        val context = ApplicationProvider.getApplicationContext<Context>()
        val fake = FakeApiInterceptor { path -> context.assets.open(path).bufferedReader().use { it.readText() } }
        val api = Retrofit.Builder()
            .baseUrl("http://fake.local/api/v1/")
            .client(OkHttpClient.Builder().addInterceptor(fake).build())
            .addConverterFactory(AppJson.asConverterFactory("application/json".toMediaType()))
            .build()
            .create<SyncApi>()
        db = inMemoryDb()
        sync = SyncRepository(db, api)
        sales = SalesRepository(db)
    }

    @After
    fun tearDown() = db.close()

    private suspend fun lot(id: String) = LotRepository(db).observeLots().first().single { it.lot.id == id }

    private fun sale(lot: String, qty: String) = DraftLine(ITEM, lot, BigDecimal(qty), BigDecimal("24.9000"))

    private suspend fun readyWithVan() {
        sync.sync()
        sync.setDeviceWarehouse(VAN)
        sync.sync()
    }

    @Test
    fun pullLoadsMasterDataAndBalanceComesFromDeviceWarehouse() = runBlocking {
        sync.sync()
        assertEquals(1, db.count("items"))
        assertEquals(3, db.count("customers"))
        assertEquals(2, db.count("warehouses"))
        assertEquals(3, db.count("lots"))
        assertEquals(listOf(BLOCKED_LOT), db.column("SELECT lot_id FROM blocked_lots"))
        assertEquals(BigDecimal("0.000"), lot(LOT).available) // depo secilmeden satilacak stok yok

        sync.setDeviceWarehouse(VAN)
        sync.sync() // imlec 0'a cekildi: lotlar yeni deponun bakiyesiyle yeniden yazilir
        assertEquals(BigDecimal("85.000"), lot(LOT).available)
        assertTrue(lot(LOT).sellable)
        assertFalse(lot(BLOCKED_LOT).sellable)
        val error = runCatching { sales.createSale(MARKET_A, listOf(sale(BLOCKED_LOT, "1"))) }.exceptionOrNull()
        assertTrue("bloke lot satilamamali: $error", (error as? SaleException)?.error is SaleError.LotNotSellable)
    }

    @Test
    fun pushIsAppliedAndResendingSameOpIdCountsAsDuplicateSuccess() = runBlocking {
        readyWithVan()
        sales.createSale(MARKET_A, listOf(sale(LOT, "2")))
        assertEquals(BigDecimal("83.000"), lot(LOT).available) // senkron oncesi: 85 sunucu + (-2) yerel

        sync.sync()
        assertEquals(listOf("applied"), db.column("SELECT status FROM sync_outbox"))
        assertEquals(listOf("1"), db.column("SELECT synced FROM sales_orders"))
        assertEquals(listOf("1"), db.column("SELECT synced FROM local_stock_movements"))
        assertEquals(BigDecimal("83.000"), lot(LOT).available) // bakiye artik pull'dan, cift dusum yok

        // yanit kayboldu senaryosu: ayni op_id tekrar gider, sunucu duplicate der, basari sayilir
        db.openHelper.writableDatabase.execSQL("UPDATE sync_outbox SET status = 'sent'")
        sync.sync()
        assertEquals(listOf("duplicate"), db.column("SELECT status FROM sync_outbox"))
        assertEquals(0, sync.observeStatus().first().pending)
        assertTrue(sync.observeStatus().first().problems.isEmpty())
        assertEquals(BigDecimal("83.000"), lot(LOT).available)
    }

    @Test
    fun offlineSaleOfLaterRecalledLotIsFlaggedAndAcknowledged() = runBlocking {
        readyWithVan()
        // cihaz bloke listesini almadan once (cevrimdisi) lotu aktif goruyordu ve satti
        db.openHelper.writableDatabase.execSQL("DELETE FROM blocked_lots")
        db.openHelper.writableDatabase.execSQL("UPDATE lots SET status = 'active' WHERE id = '$BLOCKED_LOT'")
        sales.createSale(MARKET_A, listOf(sale(BLOCKED_LOT, "1")))

        sync.sync()

        val problem = sync.observeStatus().first().problems.single()
        assertEquals("flagged", problem.status)
        assertEquals("LOT_BLOCKED_AT_SALE", problem.lastError)
        assertTrue(violationText(problem.lastError).contains("geri çağrılmış"))
        assertEquals(listOf("flagged"), db.column("SELECT status FROM sales_orders")) // sunucu kaydetti, incelemede
        assertFalse(lot(BLOCKED_LOT).sellable) // pull sonrasi tekrar bloke

        sales.acknowledge(problem.opId)
        assertTrue(sync.observeStatus().first().problems.isEmpty())
    }

    @Test
    fun saleBeyondServerStockIsFlaggedNegativeStock() = runBlocking {
        readyWithVan()
        db.master().updateLotQty(LOT, BigDecimal("100.000")) // cihazin bayat bakiyesi sunucudakinden yuksek
        sales.createSale(MARKET_A, listOf(sale(LOT, "90")))

        sync.sync()

        assertEquals("NEGATIVE_STOCK_OFFLINE", sync.observeStatus().first().problems.single().lastError)
        assertEquals(BigDecimal("-5.000"), lot(LOT).available) // sunucu otoriter: 85 - 90
    }

    @Test
    fun returnIsPushedAndRestoresStock() = runBlocking {
        readyWithVan()
        sales.createSale(MARKET_A, listOf(sale(LOT, "3")))
        sync.sync()
        val lineId = db.column("SELECT id FROM sales_lines").single()!!

        sales.createReturn(lineId, BigDecimal("1"), "bozuk ambalaj")
        sync.sync()

        assertEquals(listOf("applied"), db.column("SELECT status FROM sync_outbox WHERE entity = 'customer_return'"))
        assertEquals(listOf("1"), db.column("SELECT synced FROM customer_returns"))
        assertEquals(BigDecimal("83.000"), lot(LOT).available) // 85 - 3 + 1, sunucu bakiyesinden
    }

    @Test
    fun payloadWithFieldUnknownToServerIsRejectedAndCanBeDiscarded() = runBlocking {
        readyWithVan()
        sales.createSale(MARKET_A, listOf(sale(LOT, "1")))
        // sunucu bilinmeyen alani reddeder (DisallowUnknownFields): eski surumun line_total alani gibi
        val op = db.column("SELECT op_id FROM sync_outbox").single()!!
        db.openHelper.writableDatabase.execSQL(
            "UPDATE sync_outbox SET payload_json = REPLACE(payload_json, '\"unit_price\"', '\"line_total\":\"24.90\",\"unit_price\"')",
        )

        sync.sync()
        val problem = sync.observeStatus().first().problems.single()
        assertEquals("rejected", problem.status)
        assertEquals(listOf("rejected"), db.column("SELECT status FROM sales_orders"))
        assertEquals(BigDecimal("84.000"), lot(LOT).available) // reddedilen satis hala yerelde dusulu (temkinli)

        sales.discardRejected(op)
        assertEquals(BigDecimal("85.000"), lot(LOT).available)
        assertEquals(0, db.count("sync_outbox"))
    }

    @Test
    fun warehouseCannotChangeWhileOperationsArePending() = runBlocking {
        readyWithVan()
        sales.createSale(MARKET_A, listOf(sale(LOT, "1")))
        assertTrue(runCatching { sync.setDeviceWarehouse(MAIN) }.exceptionOrNull() is WarehouseChangeBlocked)
        sync.sync()
        sync.setDeviceWarehouse(MAIN)
        sync.sync()
        assertEquals(BigDecimal("0.000"), lot(LOT).available) // ana depoda bu lotun stogu yok
    }

    private companion object {
        const val ITEM = "01a17000-0000-7000-8000-000000000001"
        const val MARKET_A = "01a17000-0000-7000-8000-000000000101"
        const val LOT = "01a17000-0000-7000-8000-000000000201"
        const val BLOCKED_LOT = "01a17000-0000-7000-8000-000000000202"
        const val VAN = "01a17000-0000-7000-8000-000000000301"
        const val MAIN = "01a17000-0000-7000-8000-000000000302"
    }
}
