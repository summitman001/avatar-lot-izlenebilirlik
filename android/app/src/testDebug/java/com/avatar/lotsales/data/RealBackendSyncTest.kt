package com.avatar.lotsales.data

import androidx.test.ext.junit.runners.AndroidJUnit4
import com.avatar.lotsales.data.db.AppDatabase
import com.avatar.lotsales.data.net.AppJson
import com.avatar.lotsales.data.net.SyncApi
import com.avatar.lotsales.domain.SaleError
import com.avatar.lotsales.domain.SaleException
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import retrofit2.Retrofit
import retrofit2.converter.kotlinx.serialization.asConverterFactory
import retrofit2.create
import java.math.BigDecimal

/**
 * GERCEK Go API'ye karsi uctan uca senkron (FakeApi yok). Yalnizca E2E_API_URL verilince kosar:
 *   E2E_API_URL=http://127.0.0.1:8080/api/v1/ ./gradlew testDebugUnitTest --tests '*RealBackendSyncTest*'
 * Sunucuda seed verisi olmali (SUCUK-NARIN-1KG, WH-MAIN, MKT-A). Her calismada kendi alis lotunu acar.
 */
@RunWith(AndroidJUnit4::class)
class RealBackendSyncTest {
    private val baseUrl: String? = System.getenv("E2E_API_URL")
    private val http = OkHttpClient()
    private lateinit var db: AppDatabase
    private lateinit var sync: SyncRepository
    private lateinit var sales: SalesRepository

    @Before
    fun setUp() {
        assumeTrue("E2E_API_URL verilmedi; gercek backend testi atlandi", baseUrl != null)
        val api = Retrofit.Builder().baseUrl(baseUrl!!).client(http)
            .addConverterFactory(AppJson.asConverterFactory("application/json".toMediaType())).build().create<SyncApi>()
        db = inMemoryDb()
        sync = SyncRepository(db, api)
        sales = SalesRepository(db)
    }

    @After
    fun tearDown() {
        if (::db.isInitialized) db.close()
    }

    private fun call(method: String, path: String, body: String? = null): Pair<Int, JsonObject?> {
        val req = Request.Builder().url(baseUrl + path)
            .method(method, body?.toRequestBody("application/json".toMediaType()))
            .build()
        http.newCall(req).execute().use { r ->
            val text = r.body.string()
            return r.code to text.takeIf { it.startsWith("{") }?.let { AppJson.parseToJsonElement(it).jsonObject }
        }
    }

    private fun idOf(path: String, code: String): String =
        call("GET", path).second!!.getValue("items").jsonArray.map { it.jsonObject }
            .first { it.getValue("code").jsonPrimitive.content == code }.getValue("id").jsonPrimitive.content

    private suspend fun local(lotId: String) = LotRepository(db).observeLots().first().single { it.lot.id == lotId }

    private suspend fun lastOp() = db.column("SELECT op_id FROM sync_outbox ORDER BY created_at DESC, op_id DESC").first()!!

    private suspend fun statusOf(opId: String) = db.sync().outbox(opId)!!

    @Test
    fun saleReturnRecallDuplicateAndUnknownFieldAgainstRealServer() = runBlocking {
        val item = idOf("items?q=SUCUK-NARIN-1KG", "SUCUK-NARIN-1KG")
        val warehouse = idOf("warehouses", "WH-MAIN")
        val customer = idOf("customers?q=MKT-A", "MKT-A")
        val supplier = call("GET", "suppliers").second!!.getValue("items").jsonArray.first().jsonObject.getValue("id").jsonPrimitive.content
        val lotNo = "E2E-" + System.currentTimeMillis()
        val (created, lotJson) = call(
            "POST", "lots",
            """{"lot_no":"$lotNo","item_id":"$item","supplier_id":"$supplier","warehouse_id":"$warehouse","received_at":"2026-10-28","expiry_date":"2026-12-31","qty":"10","unit_price":"18.1588"}""",
        )
        assertEquals(201, created)
        val lot = lotJson!!.getValue("id").jsonPrimitive.content

        // 1) pull: yeni lot cihaz deposunun bakiyesiyle gelir (backend fix: olusturma kaydi bakiyeli)
        sync.sync()
        sync.setDeviceWarehouse(warehouse)
        sync.sync()
        assertEquals(BigDecimal("10.000"), local(lot).available)

        // 2) satis -> applied, sunucuda ayni id ile kayitli
        sales.createSale(customer, listOf(DraftLine(item, lot, BigDecimal("3"), BigDecimal("24.9000"))))
        val saleOp = lastOp()
        sync.sync()
        assertEquals("applied", statusOf(saleOp).status)
        val orderId = db.column("SELECT id FROM sales_orders").single()!!
        val (code, order) = call("GET", "sales-orders/$orderId")
        assertEquals(200, code)
        assertEquals(warehouse, order!!.getValue("warehouse_id").jsonPrimitive.content)
        assertEquals("3.000", order.getValue("lines").jsonArray.single().jsonObject.getValue("qty").jsonPrimitive.content)
        assertEquals(BigDecimal("7.000"), local(lot).available)

        // 3) iade -> applied, bakiye sunucudan 8
        sales.createReturn(db.column("SELECT id FROM sales_lines").single()!!, BigDecimal("1"), "e2e iade")
        val returnOp = lastOp()
        sync.sync()
        assertEquals("applied", statusOf(returnOp).status)
        assertEquals(BigDecimal("8.000"), local(lot).available)

        // 4) cevrimdisi satis, bu arada lot geri cagrildi -> flagged LOT_BLOCKED_AT_SALE
        sales.createSale(customer, listOf(DraftLine(item, lot, BigDecimal("1"), BigDecimal("24.9000"))))
        val blockedOp = lastOp()
        assertTrue(call("POST", "lots/$lot/block", """{"reason":"e2e geri cagirma"}""").first in 200..299)
        try {
            sync.sync()
            assertEquals("flagged", statusOf(blockedOp).status)
            assertTrue(statusOf(blockedOp).lastError!!.contains("LOT_BLOCKED_AT_SALE"))
            assertFalse(local(lot).sellable)
            val error = runCatching { sales.createSale(customer, listOf(DraftLine(item, lot, BigDecimal("1"), BigDecimal("24.9000")))) }.exceptionOrNull()
            assertTrue((error as SaleException).error is SaleError.LotNotSellable)
        } finally {
            call("POST", "lots/$lot/unblock", "{}")
        }

        // 5) yanit kayboldu: ayni op_id tekrar -> duplicate, stok ikinci kez dusmez
        db.openHelper.writableDatabase.execSQL("UPDATE sync_outbox SET status = 'sent' WHERE op_id = '$saleOp'")
        sync.sync()
        assertEquals("duplicate", statusOf(saleOp).status)
        assertEquals(BigDecimal("7.000"), local(lot).available) // 10 - 3 + 1 - 1

        // 6) sozlesme disi alan -> rejected, sunucuya hicbir sey yazilmaz
        sales.createSale(customer, listOf(DraftLine(item, lot, BigDecimal("1"), BigDecimal("24.9000"))))
        val badOp = lastOp()
        db.openHelper.writableDatabase.execSQL(
            "UPDATE sync_outbox SET payload_json = REPLACE(payload_json, '\"unit_price\"', '\"line_total\":\"24.90\",\"unit_price\"') WHERE op_id = '$badOp'",
        )
        sync.sync()
        assertEquals("rejected", statusOf(badOp).status)
        val badOrderId = AppJson.parseToJsonElement(statusOf(badOp).payloadJson).jsonObject.getValue("id").jsonPrimitive.content
        assertEquals(404, call("GET", "sales-orders/$badOrderId").first)
    }
}
