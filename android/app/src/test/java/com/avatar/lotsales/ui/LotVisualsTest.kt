package com.avatar.lotsales.ui

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.LocalDate

class LotVisualsTest {
    private val today = LocalDate.parse("2026-10-28")

    private fun level(expiry: String?) = expiryOf(expiry, today).level

    @Test
    fun expiryLevelThresholds() {
        assertEquals(ExpiryLevel.EXPIRED, level("2026-10-27"))
        assertEquals(ExpiryLevel.CRITICAL, level("2026-10-28")) // SKT gunu: bugun, henuz gecmedi
        assertEquals(ExpiryLevel.CRITICAL, level("2026-11-04")) // 7 gun
        assertEquals(ExpiryLevel.SOON, level("2026-11-05")) // 8 gun
        assertEquals(ExpiryLevel.SOON, level("2026-11-27")) // 30 gun
        assertEquals(ExpiryLevel.OK, level("2026-11-28")) // 31 gun
        assertEquals(ExpiryLevel.NONE, level(null))
        assertEquals(ExpiryLevel.NONE, level("30.11.2026"))
        assertEquals(0L, expiryOf("2026-10-28", today).days)
    }

    @Test
    fun urgentCoversExpiredThroughSoon() {
        assertTrue(expiryOf("2026-10-01", today).urgent)
        assertTrue(expiryOf("2026-11-20", today).urgent)
        assertFalse(expiryOf("2027-01-01", today).urgent)
        assertFalse(expiryOf(null, today).urgent)
    }

    @Test
    fun initialsUseTurkishUppercase() {
        assertEquals("NS", initials("Narin Sucuk 1 kg"))
        assertEquals("İK", initials("ince kıyma"))
        assertEquals("?", initials("  "))
    }
}
