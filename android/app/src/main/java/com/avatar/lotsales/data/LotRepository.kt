package com.avatar.lotsales.data

import com.avatar.lotsales.data.db.AppDatabase
import com.avatar.lotsales.data.db.BlockedLotEntity
import com.avatar.lotsales.data.db.ItemEntity
import com.avatar.lotsales.data.db.LotEntity
import com.avatar.lotsales.data.db.StockMovementEntity
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.combine
import java.math.BigDecimal
import javax.inject.Inject

/** Cihazdan satilabilen urun tipleri ve satis yapilabilen depo tipleri (sunucu ayni kurali uygular: repo.checkDeviceSale). */
val SELLABLE_ITEM_TYPES = setOf("finished", "cut_part", "by_product")
val SALE_WAREHOUSE_TYPES = setOf("internal", "vehicle")

data class LotView(
    val lot: LotEntity,
    val itemName: String,
    val baseUom: String,
    /** urun henuz senkronlanmadiysa null */
    val itemType: String?,
    /** qty_on_hand_server + senkronlanmamis yerel hareketler */
    val available: BigDecimal,
    val blocked: Boolean,
    val blockedReason: String?,
) {
    val sellable: Boolean get() = !blocked && lot.status == "active"

    /** Satis listelerinde gorunur: satilabilir urun tipi ve cihaz deposunda stok var (bloke olan da gorunur ama satilamaz). */
    val forSale: Boolean get() = itemType in SELLABLE_ITEM_TYPES && available.signum() > 0
}

/** FEFO: SKT'si en yakin once (SKT'siz en sona), sonra lot_no. */
val fefoOrder: Comparator<LotView> = compareBy<LotView, String?>(nullsLast()) { it.lot.expiryDate }.thenBy { it.lot.lotNo }

fun buildLotViews(
    lots: List<LotEntity>,
    items: List<ItemEntity>,
    blocked: List<BlockedLotEntity>,
    unsynced: List<StockMovementEntity>,
): List<LotView> {
    val itemById = items.associateBy { it.id }
    val blockedById = blocked.associateBy { it.lotId }
    val localDelta = unsynced.groupBy { it.lotId }.mapValues { (_, ms) -> ms.fold(BigDecimal.ZERO) { acc, m -> acc + m.qty } }
    return lots.map { lot ->
        val item = itemById[lot.itemId]
        LotView(
            lot = lot,
            itemName = item?.name ?: lot.itemId,
            baseUom = item?.baseUom ?: "",
            itemType = item?.itemType,
            available = lot.qtyOnHandServer + (localDelta[lot.id] ?: BigDecimal.ZERO),
            blocked = lot.id in blockedById || lot.status in setOf("blocked", "recalled"),
            blockedReason = blockedById[lot.id]?.reason,
        )
    }
}

class LotRepository @Inject constructor(private val db: AppDatabase) {
    fun observeLots(): Flow<List<LotView>> = combine(
        db.master().observeLots(),
        db.master().observeItems(),
        db.master().observeBlocked(),
        db.sales().observeUnsyncedMovements(),
    ) { lots, items, blocked, moves -> buildLotViews(lots, items, blocked, moves) }

    fun observeItems() = db.master().observeItems()

    fun observeCustomers() = db.master().observeCustomers()

    fun observeWarehouses() = db.master().observeWarehouses()
}
