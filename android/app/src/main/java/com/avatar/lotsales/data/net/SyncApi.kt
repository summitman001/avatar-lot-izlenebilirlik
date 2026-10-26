package com.avatar.lotsales.data.net

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import retrofit2.http.Body
import retrofit2.http.GET
import retrofit2.http.POST
import retrofit2.http.Query

// Sozlesme: docs/api-contract.md + docs/sync.md. Decimal'ler JSON'da STRING; burada String tasinir,
// sinirda BigDecimal'e cevrilir. Sunucu istekte bilinmeyen alani 400 ile reddeder: istek siniflarinda
// sozlesme disi alan ve varsayilan deger yoktur.

val AppJson = Json { ignoreUnknownKeys = true }

interface SyncApi {
    @POST("sync/push")
    suspend fun push(@Body body: PushRequest): PushResponse

    @GET("sync/pull")
    suspend fun pull(
        @Query("since_seq") sinceSeq: Long,
        @Query("device_id") deviceId: String,
        @Query("limit") limit: Int,
    ): PullResponse

    /** Lot bakiyesi pull payload'inda yok (sync.md); qty_on_hand buradan okunur. */
    @GET("lots")
    suspend fun lots(@Query("limit") limit: Int, @Query("offset") offset: Int): LotListResponse
}

@Serializable
data class PushRequest(
    @SerialName("device_id") val deviceId: String,
    val operations: List<PushOperation>,
)

@Serializable
data class PushOperation(
    @SerialName("op_id") val opId: String,
    val entity: String,
    val action: String,
    val payload: JsonElement,
    @SerialName("client_created_at") val clientCreatedAt: String,
)

@Serializable
data class PushResponse(
    val results: List<PushResult>,
    @SerialName("server_seq") val serverSeq: Long,
)

@Serializable
data class PushResult(
    @SerialName("op_id") val opId: String,
    val status: String,
    val message: String = "",
)

@Serializable
data class PullResponse(
    val changes: List<Change>,
    @SerialName("next_seq") val nextSeq: Long,
    @SerialName("has_more") val hasMore: Boolean = false,
    @SerialName("blocked_lot_ids") val blockedLotIds: List<String> = emptyList(),
)

@Serializable
data class Change(
    val seq: Long,
    val entity: String,
    @SerialName("entity_id") val entityId: String,
    val action: String,
    val payload: JsonElement,
)

@Serializable
data class ItemDto(
    val id: String,
    val code: String,
    val name: String,
    @SerialName("item_type") val itemType: String,
    @SerialName("base_uom") val baseUom: String,
    @SerialName("unit_weight_kg") val unitWeightKg: String? = null,
    @SerialName("default_sale_price") val defaultSalePrice: String? = null,
)

@Serializable
data class CustomerDto(
    val id: String,
    val code: String,
    val name: String,
    @SerialName("customer_type") val customerType: String,
    @SerialName("contact_name") val contactName: String? = null,
    val phone: String? = null,
)

@Serializable
data class LotDto(
    val id: String,
    @SerialName("lot_no") val lotNo: String,
    @SerialName("item_id") val itemId: String,
    val status: String,
    @SerialName("expiry_date") val expiryDate: String? = null,
    @SerialName("unit_cost") val unitCost: String,
    @SerialName("blocked_reason") val blockedReason: String? = null,
    @SerialName("qty_on_hand") val qtyOnHand: String? = null,
)

@Serializable
data class LotListResponse(val items: List<LotDto>)

/** push'ta entity=sales_order, action=create govdesi. */
@Serializable
data class SalesOrderPayload(
    val id: String,
    @SerialName("order_no") val orderNo: String,
    @SerialName("customer_id") val customerId: String,
    @SerialName("sold_at") val soldAt: String,
    val lines: List<SalesLinePayload>,
)

@Serializable
data class SalesLinePayload(
    val id: String,
    @SerialName("item_id") val itemId: String,
    @SerialName("lot_id") val lotId: String,
    val qty: String,
    @SerialName("unit_price") val unitPrice: String,
    @SerialName("line_total") val lineTotal: String,
    @SerialName("stock_movement_id") val stockMovementId: String,
)
