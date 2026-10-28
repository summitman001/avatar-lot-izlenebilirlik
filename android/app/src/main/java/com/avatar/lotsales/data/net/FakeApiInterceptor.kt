package com.avatar.lotsales.data.net

import com.avatar.lotsales.core.Decimals
import com.avatar.lotsales.data.SALE_WAREHOUSE_TYPES
import com.avatar.lotsales.data.SELLABLE_ITEM_TYPES
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.long
import okhttp3.Interceptor
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.Protocol
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import okio.Buffer
import java.math.BigDecimal

/**
 * Backend olmadan calismak icin (debug, BuildConfig.USE_FAKE_API): istekleri aga cikarmadan
 * assets/fakeapi/pull.json'dan baslayan bellek ici bir sunucu gibi cevaplar. Go sunucusunun push
 * kurallarini taklit eder (backend/internal/repo/sync.go, docs/sync.md "OFFLINE KURALI"):
 *  - payload bilinmeyen alan icerirse veya lot/satir bilinmiyorsa rejected (dedup edilmez);
 *  - hammadde/fire satisi ya da karantina/dis uretici deposundan satis rejected (repo.checkDeviceSale);
 *  - ayni op_id ikinci kez gelirse duplicate;
 *  - bloke lot satisi LOT_BLOCKED_AT_SALE, yetersiz bakiye NEGATIVE_STOCK_OFFLINE ile flagged (yine kaydedilir);
 *  - satis/iade lotun depo bakiyesini degistirir ve pull akisina bakiyeli lot upsert'i ekler.
 * Durum bellekte tutulur, uygulama yeniden baslayinca sifirlanir.
 */
class FakeApiInterceptor(private val readAsset: (String) -> String) : Interceptor {
    private val strict = Json { ignoreUnknownKeys = false } // sunucu: DisallowUnknownFields
    private val fixture by lazy { AppJson.parseToJsonElement(readAsset("fakeapi/pull.json")).jsonObject }
    private val changes by lazy { fixture.getValue("changes").jsonArray.map { it.jsonObject }.toMutableList() }
    private val blocked by lazy { fixture.getValue("blocked_lot_ids").jsonArray.map { it.jsonPrimitive.content }.toSet() }
    private val lots by lazy { latestPayloads("lot") }
    private val warehouseCodes by lazy { latestPayloads("warehouse").mapValues { it.value.getValue("code").jsonPrimitive.content } }
    private val warehouseTypes by lazy { latestPayloads("warehouse").mapValues { it.value.getValue("warehouse_type").jsonPrimitive.content } }
    private val itemTypes by lazy { latestPayloads("item").mapValues { it.value.getValue("item_type").jsonPrimitive.content } }
    private val balances by lazy { // lot_id -> (warehouse_id -> qty)
        lots.mapValues { (_, lot) ->
            lot["balances"]?.jsonArray.orEmpty().associate {
                it.jsonObject.getValue("warehouse_id").jsonPrimitive.content to BigDecimal(it.jsonObject.getValue("qty_on_hand").jsonPrimitive.content)
            }.toMutableMap()
        }.toMutableMap()
    }
    private val firstStatus = mutableMapOf<String, String>() // op_id -> ilk sonuc (yalniz kaydedilenler)
    private val soldLines = mutableMapOf<String, SoldLine>() // sales_line id -> satis bilgisi

    private data class SoldLine(val lotId: String, val warehouseId: String, val qty: BigDecimal, var returned: BigDecimal)

    @Synchronized
    override fun intercept(chain: Interceptor.Chain): Response {
        val req = chain.request()
        val path = req.url.encodedPath
        val (code, body) = when {
            req.method == "GET" && path.endsWith("/sync/pull") ->
                200 to pullBody(req.url.queryParameter("since_seq")?.toLongOrNull() ?: 0)
            req.method == "POST" && path.endsWith("/sync/push") -> {
                val buf = Buffer().also { req.body?.writeTo(it) }
                200 to pushBody(AppJson.decodeFromString(PushRequest.serializer(), buf.readUtf8()))
            }
            req.method == "GET" && path.endsWith("/health") -> 200 to """{"status":"ok"}"""
            else -> 404 to """{"error":{"code":"not_found","message":"FakeApi: rota yok"}}"""
        }
        return Response.Builder()
            .request(req)
            .protocol(Protocol.HTTP_1_1)
            .code(code)
            .message(if (code == 200) "OK" else "Not Found")
            .body(body.toResponseBody("application/json; charset=utf-8".toMediaType()))
            .build()
    }

    private fun seqOf(change: JsonObject) = change.getValue("seq").jsonPrimitive.long

    private fun latestPayloads(entity: String): Map<String, JsonObject> =
        changes.filter { it.getValue("entity").jsonPrimitive.content == entity }
            .associate { it.getValue("entity_id").jsonPrimitive.content to it.getValue("payload").jsonObject }

    private fun pullBody(sinceSeq: Long): String {
        val page = changes.filter { seqOf(it) > sinceSeq }
        return buildJsonObject {
            put("changes", JsonArray(page))
            put("next_seq", JsonPrimitive(page.maxOfOrNull(::seqOf) ?: sinceSeq))
            put("has_more", JsonPrimitive(false))
            put("blocked_lot_ids", JsonArray(blocked.map(::JsonPrimitive)))
        }.toString()
    }

    private fun pushBody(request: PushRequest): String {
        val results = request.operations.map { op ->
            firstStatus[op.opId]?.let { return@map PushResult(op.opId, "duplicate", "op_id daha once islendi (ilk sonuc: $it)") }
            val result = runCatching {
                when ("${op.entity}/${op.action}") {
                    "sales_order/create" -> applySale(op.opId, strict.decodeFromJsonElement(ServerSalesOrder.serializer(), op.payload))
                    "customer_return/create" -> applyReturn(op.opId, strict.decodeFromJsonElement(ServerCustomerReturn.serializer(), op.payload))
                    else -> PushResult(op.opId, "rejected", "desteklenmeyen islem: ${op.entity}/${op.action}")
                }
            }.getOrElse { PushResult(op.opId, "rejected", "gecersiz payload: ${it.message}") }
            if (result.status != "rejected") firstStatus[op.opId] = result.status
            result
        }
        return AppJson.encodeToString(PushResponse.serializer(), PushResponse(results, serverSeq = changes.maxOf(::seqOf)))
    }

    private fun applySale(opId: String, order: ServerSalesOrder): PushResult {
        if (order.warehouseId !in warehouseCodes) return PushResult(opId, "rejected", "warehouse_id bulunamadi")
        if (order.lines.isEmpty()) return PushResult(opId, "rejected", "lines bos olamaz")
        warehouseTypes.getValue(order.warehouseId).let {
            if (it !in SALE_WAREHOUSE_TYPES) return PushResult(opId, "rejected", "cihazdan $it tipindeki depodan satis yapilamaz (DEVICE_SALE_WAREHOUSE)")
        }
        order.lines.forEachIndexed { i, line ->
            itemTypes[line.itemId]?.let {
                if (it !in SELLABLE_ITEM_TYPES) return PushResult(opId, "rejected", "lines[$i]: $it tipindeki urun cihazdan satilamaz (DEVICE_SALE_ITEM_TYPE)")
            }
        }
        order.lines.forEach { if (it.lotId !in lots) return PushResult(opId, "rejected", "lot bulunamadi: ${it.lotId}") }
        val violations = linkedSetOf<String>()
        order.lines.forEach { line ->
            val qty = BigDecimal(line.qty)
            val wh = balances.getOrPut(line.lotId) { mutableMapOf() }
            val left = (wh[order.warehouseId] ?: BigDecimal.ZERO) - qty
            if (line.lotId in blocked) violations += "LOT_BLOCKED_AT_SALE"
            if (left.signum() < 0) violations += "NEGATIVE_STOCK_OFFLINE"
            wh[order.warehouseId] = left
            line.id?.let { soldLines[it] = SoldLine(line.lotId, order.warehouseId, qty, BigDecimal.ZERO) }
        }
        order.lines.map { it.lotId }.distinct().forEach(::emitLot)
        return if (violations.isEmpty()) PushResult(opId, "applied", "satis kaydedildi: ${order.orderNo ?: order.id}")
        else PushResult(opId, "flagged", violations.joinToString(","))
    }

    private fun applyReturn(opId: String, ret: ServerCustomerReturn): PushResult {
        val sold = ret.salesLineId?.let(soldLines::get) ?: return PushResult(opId, "rejected", "sales_line bulunamadi")
        val qty = BigDecimal(ret.qty)
        if (sold.returned + qty > sold.qty) return PushResult(opId, "rejected", "iade miktari satilandan fazla")
        sold.returned += qty
        val wh = balances.getValue(sold.lotId)
        wh[sold.warehouseId] = (wh[sold.warehouseId] ?: BigDecimal.ZERO) + qty
        emitLot(sold.lotId)
        return PushResult(opId, "applied", "iade kaydedildi")
    }

    /** Sunucu gibi: bakiyesi degisen lot icin yeni seq ile bakiyeli upsert. */
    private fun emitLot(lotId: String) {
        val wh = balances.getValue(lotId)
        val payload = JsonObject(
            lots.getValue(lotId) + mapOf(
                "qty_on_hand" to JsonPrimitive(wh.values.fold(BigDecimal.ZERO, BigDecimal::add).setScale(Decimals.QTY_SCALE).toPlainString()),
                "balances" to JsonArray(
                    wh.map { (whId, q) ->
                        buildJsonObject {
                            put("warehouse_id", JsonPrimitive(whId))
                            put("warehouse_code", JsonPrimitive(warehouseCodes[whId].orEmpty()))
                            put("qty_on_hand", JsonPrimitive(q.setScale(Decimals.QTY_SCALE).toPlainString()))
                        }
                    },
                ),
            ),
        )
        changes += buildJsonObject {
            put("seq", JsonPrimitive(changes.maxOf(::seqOf) + 1))
            put("entity", JsonPrimitive("lot"))
            put("entity_id", JsonPrimitive(lotId))
            put("action", JsonPrimitive("upsert"))
            put("payload", payload)
        }
    }
}

// Go sunucusunun kabul ettigi alanlar (backend/internal/domain/sales.go); fazlasi strict decode'da patlar.
@Serializable
private data class ServerSalesOrder(
    val id: String? = null,
    @SerialName("order_no") val orderNo: String? = null,
    @SerialName("customer_id") val customerId: String,
    @SerialName("warehouse_id") val warehouseId: String,
    @SerialName("device_id") val deviceId: String? = null,
    @SerialName("sold_at") val soldAt: String,
    val lines: List<ServerSalesLine>,
    @SerialName("create_shipment") val createShipment: Boolean? = null,
)

@Serializable
private data class ServerSalesLine(
    val id: String? = null,
    @SerialName("item_id") val itemId: String,
    @SerialName("lot_id") val lotId: String,
    val qty: String,
    @SerialName("unit_price") val unitPrice: String,
)

@Serializable
private data class ServerCustomerReturn(
    val id: String? = null,
    @SerialName("return_no") val returnNo: String? = null,
    @SerialName("customer_id") val customerId: String,
    @SerialName("sales_line_id") val salesLineId: String? = null,
    @SerialName("lot_id") val lotId: String? = null,
    val qty: String,
    val reason: String,
    @SerialName("returned_at") val returnedAt: String? = null,
)
