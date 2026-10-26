package com.avatar.lotsales.data

import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import android.content.Context
import com.avatar.lotsales.data.db.AppDatabase
import com.avatar.lotsales.data.db.LotEntity
import com.avatar.lotsales.data.net.AppJson
import com.avatar.lotsales.data.net.FakeApiInterceptor
import com.avatar.lotsales.data.net.SyncApi
import com.avatar.lotsales.domain.SaleError
import com.avatar.lotsales.domain.SaleException
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

/** Debug assets'teki FakeApi fixture'lariyla uctan uca senkron (push -> pull -> bakiye). */
@RunWith(AndroidJUnit4::class)
class SyncRepositoryTest {
    private lateinit var db: AppDatabase
    private lateinit var sync: SyncRepository

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
    }

    @After
    fun tearDown() = db.close()

    private suspend fun lot(id: String) = LotRepository(db).observeLots().first().single { it.lot.id == id }

    private fun sale(lot: String, qty: String) = DraftLine(ITEM, lot, BigDecimal(qty), BigDecimal("24.9000"))

    @Test
    fun pullLoadsMasterDataAndBlockedLotCannotBeSold() = runBlocking {
        sync.sync()

        assertEquals(1, db.count("items"))
        assertEquals(3, db.count("customers"))
        assertEquals(3, db.count("lots"))
        assertEquals(listOf(BLOCKED_LOT), db.column("SELECT lot_id FROM blocked_lots"))
        assertEquals(listOf("8"), db.column("SELECT value FROM sync_state WHERE `key` = 'last_pull_seq'"))
        assertEquals(BigDecimal("85.000"), lot(LOT).available) // bakiye GET /lots'tan
        assertTrue(lot(LOT).sellable)
        assertFalse(lot(BLOCKED_LOT).sellable)

        val error = runCatching { SalesRepository(db).createSale(MARKET_A, listOf(sale(BLOCKED_LOT, "1"))) }.exceptionOrNull()
        assertTrue("bloke lot satilamamali: $error", (error as? SaleException)?.error is SaleError.LotNotSellable)
    }

    @Test
    fun pushMarksAppliedAndResendingSameOpIdCountsAsDuplicateSuccess() = runBlocking {
        sync.sync()
        SalesRepository(db).createSale(MARKET_A, listOf(sale(LOT, "2")))
        assertEquals(BigDecimal("83.000"), lot(LOT).available) // senkron oncesi: 85 sunucu + (-2) yerel

        sync.sync()
        assertEquals(listOf("applied"), db.column("SELECT status FROM sync_outbox"))
        assertEquals(listOf("1"), db.column("SELECT synced FROM sales_orders"))
        assertEquals(listOf("1"), db.column("SELECT synced FROM local_stock_movements"))
        assertEquals(BigDecimal("83.000"), lot(LOT).available) // artik tamami sunucu bakiyesinde, cift dusum yok

        // yanit kayboldu senaryosu: ayni op_id tekrar gider, sunucu duplicate der, basari sayilir
        db.openHelper.writableDatabase.execSQL("UPDATE sync_outbox SET status = 'sent'")
        sync.sync()
        assertEquals(listOf("duplicate"), db.column("SELECT status FROM sync_outbox"))
        assertEquals(0, sync.observeStatus().first().pending)
        assertTrue(sync.observeStatus().first().problems.isEmpty())
        assertEquals(BigDecimal("83.000"), lot(LOT).available)
    }

    @Test
    fun offlineSaleOfLaterRecalledLotIsFlaggedForReview() = runBlocking {
        // cihaz bloke listesini almadan once (cevrimdisi) lotu aktif goruyordu ve satti
        db.master().insertLot(LotEntity(BLOCKED_LOT, "SUC-2026-099", ITEM, "active", "2026-11-30", BigDecimal("18.2000"), BigDecimal("40.000"), 0))
        db.master().upsertCustomer(com.avatar.lotsales.data.db.CustomerEntity(MARKET_A, "MUS-A", "Market A", "market", null, null))
        SalesRepository(db).createSale(MARKET_A, listOf(sale(BLOCKED_LOT, "1")))

        sync.sync()

        val status = sync.observeStatus().first()
        assertEquals(listOf("flagged"), status.problems.map { it.status })
        assertTrue(status.problems.single().lastError!!.contains("recall_violation"))
        assertEquals(listOf("1"), db.column("SELECT synced FROM sales_orders")) // sunucu kaydetti
        assertFalse(lot(BLOCKED_LOT).sellable) // pull sonrasi artik secilemez
    }

    private companion object {
        const val ITEM = "01a17000-0000-7000-8000-000000000001"
        const val MARKET_A = "01a17000-0000-7000-8000-000000000101"
        const val LOT = "01a17000-0000-7000-8000-000000000201"
        const val BLOCKED_LOT = "01a17000-0000-7000-8000-000000000202"
    }
}
