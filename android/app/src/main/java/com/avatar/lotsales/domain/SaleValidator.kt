package com.avatar.lotsales.domain

import java.math.BigDecimal

sealed class SaleError(val message: String) {
    class LotNotSellable(lotNo: String) : SaleError("Lot $lotNo bloke veya geri çağrılmış; satılamaz")
    data object InvalidQty : SaleError("Miktar sıfırdan büyük olmalı (en fazla 3 ondalık)")
    data object InvalidPrice : SaleError("Birim fiyat negatif olamaz (en fazla 4 ondalık)")
    class InsufficientStock(lotNo: String, available: BigDecimal) :
        SaleError("Lot $lotNo için kullanılabilir miktar ${available.toPlainString()}")
    data object NoCustomer : SaleError("Müşteri seçilmeli")
    data object NoLines : SaleError("En az bir satır eklenmeli")
    data object UnknownLot : SaleError("Lot bulunamadı")
    data object NoWarehouse : SaleError("Cihaz deposu seçilmedi (Senkron ekranından seçin)")
    class ReturnExceedsSold(max: BigDecimal) : SaleError("İade miktarı satılandan fazla olamaz (en fazla ${max.toPlainString()})")
    data object ReasonRequired : SaleError("İade nedeni zorunlu (en fazla 400 karakter)")
    data object OrderRejected : SaleError("Sunucunun reddettiği satış iade edilemez")
    data object UnknownLine : SaleError("Satış satırı bulunamadı")
}

class SaleException(val error: SaleError) : Exception(error.message)

/** Saf kurallar: hem ekranda (satir eklerken) hem repository transaction'inda (onayda) cagrilir. */
object SaleValidator {
    /**
     * @param alreadyInDraft ayni lot icin taslakta (veya ayni sipariste) zaten ayrilmis miktar
     * @param qty null = parse edilemeyen veya olcegi asan girdi
     */
    fun validateLine(
        lotNo: String,
        sellable: Boolean,
        available: BigDecimal,
        alreadyInDraft: BigDecimal,
        qty: BigDecimal?,
        unitPrice: BigDecimal?,
    ): SaleError? = when {
        !sellable -> SaleError.LotNotSellable(lotNo)
        qty == null || qty.signum() <= 0 -> SaleError.InvalidQty
        unitPrice == null || unitPrice.signum() < 0 -> SaleError.InvalidPrice
        alreadyInDraft + qty > available -> SaleError.InsufficientStock(lotNo, available - alreadyInDraft)
        else -> null
    }

    /** Sunucu kurali: iade, satilan miktar - daha once iade edilen miktari asamaz; neden zorunlu (<= 400). */
    fun validateReturn(sold: BigDecimal, alreadyReturned: BigDecimal, qty: BigDecimal?, reason: String): SaleError? = when {
        qty == null || qty.signum() <= 0 -> SaleError.InvalidQty
        alreadyReturned + qty > sold -> SaleError.ReturnExceedsSold(sold - alreadyReturned)
        reason.isBlank() || reason.trim().length > 400 -> SaleError.ReasonRequired
        else -> null
    }
}

/** Sunucunun flagged mesajindaki ihlal kodlari (docs/android-integration.md) -> kullaniciya metin. */
fun violationText(message: String?): String =
    message.orEmpty().split(',').map { it.trim() }.filter { it.isNotEmpty() }.joinToString("\n") { code ->
        when (code) {
            "LOT_BLOCKED_AT_SALE" -> "Bu lot geri çağrılmış/bloke; satış incelemeye alındı."
            "NEGATIVE_STOCK_OFFLINE" -> "Stok yetersizdi, negatif bakiye oluştu; satış incelemeye alındı."
            else -> code
        }
    }
