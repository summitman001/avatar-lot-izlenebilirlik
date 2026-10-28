package com.avatar.lotsales.core

import com.avatar.lotsales.data.db.Converters
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import java.math.BigDecimal

class DecimalsTest {
    private val converters = Converters()

    @Test
    fun typeConverterRoundTripKeepsValueAndScale() {
        listOf("18.1588", "-2.000", "0.000", "24.90", "1543.50", "123456789012.1234").forEach { s ->
            val stored = converters.fromDecimal(BigDecimal(s))
            assertEquals(s, stored)
            assertEquals(BigDecimal(s), converters.toDecimal(stored))
            assertEquals(BigDecimal(s).scale(), converters.toDecimal(stored)!!.scale())
        }
        assertNull(converters.fromDecimal(null))
        assertNull(converters.toDecimal(null))
    }

    @Test
    fun typeConverterNeverUsesScientificNotation() {
        assertEquals("0.0000001", converters.fromDecimal(BigDecimal("1E-7")))
    }

    @Test
    fun lineTotalRoundsHalfUpToTwoDecimals() {
        // docs/assumptions.md: 18,500 kg x 11,75 = 217,375 -> 217,38
        assertEquals(BigDecimal("217.38"), Decimals.lineTotal(BigDecimal("18.500"), BigDecimal("11.7500")))
        // 85 x 18,1588 = 1543,498 -> 1543,50
        assertEquals(BigDecimal("1543.50"), Decimals.lineTotal(BigDecimal("85.000"), BigDecimal("18.1588")))
        assertEquals(BigDecimal("49.80"), Decimals.lineTotal(BigDecimal("2.000"), BigDecimal("24.9000")))
    }

    @Test
    fun parseAcceptsCommaAndRejectsExcessScale() {
        assertEquals("2.000", Decimals.parse("2", Decimals.QTY_SCALE)!!.toPlainString())
        assertEquals("2.500", Decimals.parse(" 2,5 ", Decimals.QTY_SCALE)!!.toPlainString())
        assertEquals("24.9000", Decimals.parse("24.9", Decimals.PRICE_SCALE)!!.toPlainString())
        assertNull(Decimals.parse("1.0001", Decimals.QTY_SCALE)) // sunucu gibi: sessiz yuvarlama yok
        assertNull(Decimals.parse("abc", Decimals.QTY_SCALE))
        assertNull(Decimals.parse("", Decimals.QTY_SCALE))
    }
}
