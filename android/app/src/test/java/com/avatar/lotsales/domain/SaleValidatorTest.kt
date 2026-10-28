package com.avatar.lotsales.domain

import com.avatar.lotsales.data.buildLotViews
import com.avatar.lotsales.data.db.BlockedLotEntity
import com.avatar.lotsales.data.db.ItemEntity
import com.avatar.lotsales.data.db.LotEntity
import com.avatar.lotsales.data.db.StockMovementEntity
import com.avatar.lotsales.data.fefoOrder
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.math.BigDecimal

class SaleValidatorTest {
    private fun d(s: String) = BigDecimal(s)

    private fun validate(sellable: Boolean = true, available: String = "85.000", already: String = "0", qty: String? = "1.000", price: String? = "24.9000") =
        SaleValidator.validateLine("SUC-2026-045", sellable, d(available), d(already), qty?.let(::d), price?.let(::d))

    @Test
    fun blockedLotIsRejected() {
        assertTrue(validate(sellable = false) is SaleError.LotNotSellable)
    }

    @Test
    fun zeroNegativeOrUnparsableQtyIsRejected() {
        assertEquals(SaleError.InvalidQty, validate(qty = "0.000"))
        assertEquals(SaleError.InvalidQty, validate(qty = "-1.000"))
        assertEquals(SaleError.InvalidQty, validate(qty = null))
    }

    @Test
    fun qtyCannotExceedAvailableIncludingDraftLines() {
        assertTrue(validate(available = "85.000", already = "80.000", qty = "6.000") is SaleError.InsufficientStock)
        assertNull(validate(available = "85.000", already = "80.000", qty = "5.000")) // tam sinir kabul
        assertTrue(validate(available = "0.000", qty = "1.000") is SaleError.InsufficientStock)
    }

    @Test
    fun negativeOrMissingPriceIsRejected() {
        assertEquals(SaleError.InvalidPrice, validate(price = "-0.0100"))
        assertEquals(SaleError.InvalidPrice, validate(price = null))
        assertNull(validate(price = "0.0000"))
    }

    @Test
    fun returnCannotExceedSoldMinusAlreadyReturnedAndNeedsReason() {
        assertNull(SaleValidator.validateReturn(d("3.000"), d("1.000"), d("2.000"), "bozuk"))
        assertTrue(SaleValidator.validateReturn(d("3.000"), d("1.000"), d("2.001"), "bozuk") is SaleError.ReturnExceedsSold)
        assertEquals(SaleError.InvalidQty, SaleValidator.validateReturn(d("3.000"), d("0"), d("0"), "bozuk"))
        assertEquals(SaleError.ReasonRequired, SaleValidator.validateReturn(d("3.000"), d("0"), d("1"), "   "))
        assertEquals(SaleError.ReasonRequired, SaleValidator.validateReturn(d("3.000"), d("0"), d("1"), "x".repeat(401)))
    }

    @Test
    fun violationCodesFromServerBecomeReadableText() {
        val text = violationText("LOT_BLOCKED_AT_SALE,NEGATIVE_STOCK_OFFLINE")
        assertEquals(2, text.lines().size)
        assertTrue(text.contains("geri çağrılmış"))
        assertTrue(text.contains("negatif bakiye"))
        assertEquals("BILINMEYEN_KOD", violationText("BILINMEYEN_KOD"))
        assertEquals("", violationText(null))
    }

    @Test
    fun lotViewAvailableIsServerQtyPlusUnsyncedMovementsAndBlockedIsNotSellable() {
        val item = ItemEntity("i1", "SUCUK", "Narin Sucuk", "finished", "adet", d("1.000"), d("24.9000"))
        fun lot(id: String, expiry: String?, status: String = "active") =
            LotEntity(id, "LOT-$id", "i1", status, expiry, d("18.1588"), d("85.000"), 1)
        val lots = listOf(lot("a", "2026-12-15"), lot("b", null), lot("c", "2026-11-20"), lot("d", "2026-11-01", status = "recalled"))
        val moves = listOf(
            StockMovementEntity("m1", "a", "sale_out", d("-2.000"), "sales_order", "o1", "2026-10-26T10:00:00Z", synced = false),
            StockMovementEntity("m2", "a", "sale_out", d("-0.500"), "sales_order", "o2", "2026-10-26T10:01:00Z", synced = false),
        )
        val views = buildLotViews(lots, listOf(item), listOf(BlockedLotEntity("c", "geri cagirma", "2026-10-26T10:00:00Z")), moves)
            .associateBy { it.lot.id }

        assertEquals(d("82.500"), views.getValue("a").available)
        assertTrue(views.getValue("a").sellable)
        assertFalse(views.getValue("c").sellable) // blocked_lots listesinde
        assertFalse(views.getValue("d").sellable) // status recalled
        assertEquals("geri cagirma", views.getValue("c").blockedReason)
        // FEFO: SKT'si en yakin once, SKT'siz en sonda
        assertEquals(listOf("d", "c", "a", "b"), views.values.sortedWith(fefoOrder).map { it.lot.id })
    }
}
