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
}

class SaleException(val error: SaleError) : Exception(error.message)

/** Saf kurallar: hem ekranda (satir eklerken) hem SalesRepository transaction'inda (onayda) cagrilir. */
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
}
