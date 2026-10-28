package com.avatar.lotsales.core

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.UUID

class Uuid7Test {
    @Test
    fun idsAreValidV7LowercaseAndStrictlyIncreasingWithinOneMillisecond() {
        val gen = Uuid7Generator()
        val ms = 1_761_480_000_000L
        val ids = List(10_000) { gen.generate(ms) } // 4096'yi asar: sayac tasmasi da denenir

        assertEquals(ids.sorted(), ids)
        assertEquals(ids.size, ids.toSet().size)
        ids.forEach { id ->
            val u = UUID.fromString(id)
            assertEquals(7, u.version())
            assertEquals(2, u.variant()) // RFC 9562 varyanti (10xx)
            assertEquals(id.lowercase(), id)
            assertEquals(36, id.length)
        }
        assertEquals(ms, UUID.fromString(ids.first()).mostSignificantBits ushr 16)
    }

    @Test
    fun clockGoingBackwardsStaysMonotonic() {
        val gen = Uuid7Generator()
        val later = gen.generate(2_000_000L)
        val earlierClock = gen.generate(1_000_000L)
        assertTrue(earlierClock > later)
    }

    @Test
    fun timestampFollowsClock() {
        val gen = Uuid7Generator()
        val a = gen.generate(1_000L)
        val b = gen.generate(5_000L)
        assertEquals(5_000L, UUID.fromString(b).mostSignificantBits ushr 16)
        assertTrue(b > a)
    }
}
