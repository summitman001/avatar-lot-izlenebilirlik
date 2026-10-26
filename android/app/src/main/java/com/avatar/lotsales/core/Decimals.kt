package com.avatar.lotsales.core

import java.math.BigDecimal
import java.math.RoundingMode

/** Sunucuyla ayni olcekler (docs/api-contract.md): miktar 3, birim fiyat/maliyet 4, satir tutari 2 ondalik, HALF_UP. */
object Decimals {
    const val QTY_SCALE = 3
    const val PRICE_SCALE = 4
    const val MONEY_SCALE = 2

    val ZERO_QTY: BigDecimal = BigDecimal.ZERO.setScale(QTY_SCALE)

    /** line_total = round2(qty x unit_price), Go'daki domain.LineTotal ile ayni. */
    fun lineTotal(qty: BigDecimal, unitPrice: BigDecimal): BigDecimal =
        qty.multiply(unitPrice).setScale(MONEY_SCALE, RoundingMode.HALF_UP)

    /** Kullanici girdisi ("2", "2,5"); olcekten fazla ondalik null doner (sunucu gibi sessiz yuvarlama yok). */
    fun parse(text: String, maxScale: Int): BigDecimal? =
        text.trim().replace(',', '.').toBigDecimalOrNull()
            ?.takeIf { it.scale() <= maxScale }
            ?.setScale(maxScale)
}
