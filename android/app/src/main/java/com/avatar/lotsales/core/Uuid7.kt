package com.avatar.lotsales.core

import java.security.SecureRandom
import java.util.Random
import java.util.UUID

/** Uygulama genelinde tek uretec: ayni surecteki tum ID'ler artan sirada. */
object Uuid7 {
    private val generator = Uuid7Generator()

    fun generate(): String = generator.generate(System.currentTimeMillis())
}

/**
 * RFC 9562 UUIDv7: 48 bit milisaniye + surum (7) + 12 bit sayac + varyant (10) + 62 bit rastgele.
 * Ayni milisaniyede (veya saat geri giderse) sayac artar; uretilen ID'ler string olarak da kesin
 * artan siradadir. Sunucu (domain.ParseID) yalnizca kucuk harfli UUIDv7 kabul eder.
 */
class Uuid7Generator(private val random: Random = SecureRandom()) {
    private var lastMs = -1L
    private var counter = 0

    @Synchronized
    fun generate(nowMs: Long): String {
        var ms = nowMs
        if (ms > lastMs) {
            counter = random.nextInt(0x800) // ust yari ayni ms icindeki artislara kalir
        } else {
            ms = lastMs
            if (++counter > 0xFFF) {
                ms++
                counter = 0
            }
        }
        lastMs = ms
        val msb = (ms shl 16) or 0x7000L or counter.toLong()
        val lsb = (random.nextLong() ushr 2) or Long.MIN_VALUE
        return UUID(msb, lsb).toString()
    }
}
