package com.avatar.lotsales.data.net

import com.avatar.lotsales.core.Decimals
import kotlinx.serialization.json.JsonArray
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
 * assets/fakeapi/ fixture'larindan cevaplar. Sunucu kurallarini taklit eder: ayni op_id ikinci kez
 * gelirse duplicate; bloke lot satilirsa flagged (recall_violation); applied/flagged satis bakiyeden duser.
 * Durum bellekte tutulur, uygulama yeniden baslayinca sifirlanir.
 */
class FakeApiInterceptor(private val readAsset: (String) -> String) : Interceptor {
    private val pull by lazy { AppJson.parseToJsonElement(readAsset("fakeapi/pull.json")).jsonObject }
    private val lots by lazy { AppJson.decodeFromString(LotListResponse.serializer(), readAsset("fakeapi/lots.json")).items }
    private val firstStatus = mutableMapOf<String, String>() // op_id -> ilk sonuc
    private val sold = mutableMapOf<String, BigDecimal>() // lot_id -> satilan miktar

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
            req.method == "GET" && path.endsWith("/lots") ->
                200 to lotsBody(req.url.queryParameter("offset")?.toIntOrNull() ?: 0)
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

    private fun blockedIds(): List<String> = pull.getValue("blocked_lot_ids").jsonArray.map { it.jsonPrimitive.content }

    private fun pullBody(sinceSeq: Long): String {
        val changes = pull.getValue("changes").jsonArray.filter { it.jsonObject.getValue("seq").jsonPrimitive.long > sinceSeq }
        val next = changes.maxOfOrNull { it.jsonObject.getValue("seq").jsonPrimitive.long } ?: sinceSeq
        return buildJsonObject {
            put("changes", JsonArray(changes))
            put("next_seq", JsonPrimitive(next))
            put("has_more", JsonPrimitive(false))
            put("blocked_lot_ids", pull.getValue("blocked_lot_ids"))
        }.toString()
    }

    private fun pushBody(request: PushRequest): String {
        val blocked = blockedIds().toSet()
        val results = request.operations.map { op ->
            firstStatus[op.opId]?.let { return@map PushResult(op.opId, "duplicate", "op_id daha once islendi (ilk sonuc: $it)") }
            val result = if (op.entity == "sales_order" && op.action == "create") {
                val order = AppJson.decodeFromJsonElement(SalesOrderPayload.serializer(), op.payload)
                order.lines.forEach { sold[it.lotId] = (sold[it.lotId] ?: BigDecimal.ZERO) + BigDecimal(it.qty) }
                if (order.lines.any { it.lotId in blocked }) {
                    PushResult(op.opId, "flagged", "recall_violation: bloke/geri cagrilmis lot satildi")
                } else {
                    PushResult(op.opId, "applied", "")
                }
            } else {
                PushResult(op.opId, "rejected", "FakeApi: desteklenmeyen islem ${op.entity}/${op.action}")
            }
            firstStatus[op.opId] = result.status
            result
        }
        return AppJson.encodeToString(PushResponse.serializer(), PushResponse(results, serverSeq = 0))
    }

    private fun lotsBody(offset: Int): String {
        val page = if (offset > 0) emptyList() else lots.map { lot ->
            val onHand = BigDecimal(lot.qtyOnHand ?: "0") - (sold[lot.id] ?: BigDecimal.ZERO)
            lot.copy(qtyOnHand = onHand.setScale(Decimals.QTY_SCALE).toPlainString())
        }
        return AppJson.encodeToString(LotListResponse.serializer(), LotListResponse(page))
    }
}
