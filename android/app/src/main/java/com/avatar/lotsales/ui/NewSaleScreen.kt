package com.avatar.lotsales.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.navigation.toRoute
import com.avatar.lotsales.core.Decimals
import com.avatar.lotsales.data.DraftLine
import com.avatar.lotsales.data.LotRepository
import com.avatar.lotsales.data.LotView
import com.avatar.lotsales.data.SalesRepository
import com.avatar.lotsales.data.db.CustomerEntity
import com.avatar.lotsales.data.db.ItemEntity
import com.avatar.lotsales.data.fefoOrder
import com.avatar.lotsales.domain.SaleError
import com.avatar.lotsales.domain.SaleException
import com.avatar.lotsales.domain.SaleValidator
import com.avatar.lotsales.sync.SyncScheduler
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import java.math.BigDecimal
import javax.inject.Inject

data class DraftLineUi(val line: DraftLine, val lotNo: String, val itemName: String, val uom: String) {
    val lineTotal: BigDecimal get() = Decimals.lineTotal(line.qty, line.unitPrice)
}

data class SaleForm(
    val customerId: String? = null,
    val itemId: String? = null,
    val lotId: String? = null,
    val qty: String = "",
    val price: String = "",
    val lines: List<DraftLineUi> = emptyList(),
    val error: String? = null,
    val info: String? = null,
)

data class NewSaleUi(
    val form: SaleForm = SaleForm(),
    val customers: List<CustomerEntity> = emptyList(),
    val items: List<ItemEntity> = emptyList(),
    /** secili urunun lotlari, FEFO sirali (bloke olanlar da listelenir, secilince hata verir) */
    val itemLots: List<LotView> = emptyList(),
) {
    val customer get() = customers.find { it.id == form.customerId }
    val item get() = items.find { it.id == form.itemId }
    val lot get() = itemLots.find { it.lot.id == form.lotId }
    val fefoSuggestion get() = itemLots.firstOrNull { it.sellable && it.available.signum() > 0 }
    val total: BigDecimal get() = form.lines.fold(BigDecimal.ZERO.setScale(Decimals.MONEY_SCALE)) { acc, l -> acc + l.lineTotal }
}

@HiltViewModel
class NewSaleViewModel @Inject constructor(
    savedState: SavedStateHandle,
    lotRepo: LotRepository,
    private val sales: SalesRepository,
    private val scheduler: SyncScheduler,
) : ViewModel() {
    private val form = MutableStateFlow(SaleForm())
    private val lots = lotRepo.observeLots().stateIn(viewModelScope, SharingStarted.Eagerly, emptyList())
    private val items = lotRepo.observeItems().stateIn(viewModelScope, SharingStarted.Eagerly, emptyList())

    val ui = combine(form, lotRepo.observeCustomers(), items, lots) { f, customers, items, all ->
        NewSaleUi(f, customers, items, all.filter { it.lot.itemId == f.itemId }.sortedWith(fefoOrder))
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), NewSaleUi())

    init {
        // Lot listesinden gelindiyse o lot (ve urunu) secili baslar.
        savedState.toRoute<NewSaleRoute>().lotId?.let { lotId ->
            viewModelScope.launch { lots.first { l -> l.any { it.lot.id == lotId } }.first { it.lot.id == lotId }.let(::pickLot) }
        }
    }

    fun selectCustomer(c: CustomerEntity) = form.update { it.copy(customerId = c.id, error = null, info = null) }

    /** Urun secilince FEFO: SKT'si en yakin, satilabilir ve stoklu lot onerilir. */
    fun selectItem(item: ItemEntity) {
        val suggestion = lots.value.filter { it.lot.itemId == item.id }.sortedWith(fefoOrder)
            .firstOrNull { it.sellable && it.available.signum() > 0 }
        form.update {
            it.copy(itemId = item.id, lotId = suggestion?.lot?.id, price = item.defaultSalePrice?.toPlainString().orEmpty(), error = null, info = null)
        }
    }

    fun pickLot(lot: LotView) {
        if (!lot.sellable) {
            form.update { it.copy(error = SaleError.LotNotSellable(lot.lot.lotNo).message) }
            return
        }
        val item = items.value.find { it.id == lot.lot.itemId }
        form.update {
            it.copy(
                itemId = lot.lot.itemId, lotId = lot.lot.id, error = null, info = null,
                price = it.price.ifEmpty { item?.defaultSalePrice?.toPlainString().orEmpty() },
            )
        }
    }

    fun setQty(v: String) = form.update { it.copy(qty = v, error = null) }

    fun setPrice(v: String) = form.update { it.copy(price = v, error = null) }

    fun addLine() {
        val state = ui.value
        val f = state.form
        val lot = state.lot ?: return form.update { it.copy(error = "Lot seçin") }
        val qty = Decimals.parse(f.qty, Decimals.QTY_SCALE)
        val price = Decimals.parse(f.price, Decimals.PRICE_SCALE)
        val already = f.lines.filter { it.line.lotId == lot.lot.id }.fold(BigDecimal.ZERO) { acc, l -> acc + l.line.qty }
        SaleValidator.validateLine(lot.lot.lotNo, lot.sellable, lot.available, already, qty, price)?.let { e ->
            return form.update { it.copy(error = e.message) }
        }
        val line = DraftLineUi(DraftLine(lot.lot.itemId, lot.lot.id, qty!!, price!!), lot.lot.lotNo, lot.itemName, lot.baseUom)
        form.update { it.copy(lines = it.lines + line, qty = "", error = null, info = null) }
    }

    fun removeLine(index: Int) = form.update { it.copy(lines = it.lines.filterIndexed { i, _ -> i != index }) }

    fun confirm() {
        val f = form.value
        val customerId = f.customerId ?: return form.update { it.copy(error = SaleError.NoCustomer.message) }
        viewModelScope.launch {
            try {
                val orderNo = sales.createSale(customerId, f.lines.map { it.line })
                form.value = SaleForm(customerId = customerId, info = "Satış kaydedildi: $orderNo (senkron kuyruğunda)")
                scheduler.syncNow()
            } catch (e: SaleException) {
                form.update { it.copy(error = e.error.message) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                form.update { it.copy(error = "Satış kaydedilemedi: ${e.message}") } // transaction geri alindi
            }
        }
    }
}

@Composable
fun NewSaleScreen(vm: NewSaleViewModel = hiltViewModel()) {
    val ui by vm.ui.collectAsStateWithLifecycle()
    val f = ui.form
    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Text("Müşteri", style = MaterialTheme.typography.labelLarge)
        Picker("Müşteri", ui.customer?.name, ui.customers, { it.name }, vm::selectCustomer)
        Text("Ürün", style = MaterialTheme.typography.labelLarge)
        Picker("Ürün", ui.item?.name, ui.items, { it.name }, vm::selectItem)
        Text("Lot (FEFO: SKT'si en yakın önce)", style = MaterialTheme.typography.labelLarge)
        Picker(
            "Lot",
            ui.lot?.let { "${it.lot.lotNo} · SKT ${it.lot.expiryDate ?: "—"}" },
            ui.itemLots,
            { lot ->
                buildString {
                    append("${lot.lot.lotNo} · SKT ${lot.lot.expiryDate ?: "—"} · ${lot.available.display()} ${lot.baseUom}")
                    if (!lot.sellable) append(" · BLOKE")
                    if (lot == ui.fefoSuggestion) append(" · FEFO önerisi")
                }
            },
            vm::pickLot,
        )
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            OutlinedTextField(
                f.qty, vm::setQty, label = { Text("Miktar") }, singleLine = true, modifier = Modifier.weight(1f),
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
            )
            OutlinedTextField(
                f.price, vm::setPrice, label = { Text("Birim fiyat") }, singleLine = true, modifier = Modifier.weight(1f),
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
            )
        }
        Button(onClick = vm::addLine, modifier = Modifier.fillMaxWidth()) { Text("Satır ekle") }
        f.error?.let { Text(it, color = MaterialTheme.colorScheme.error) }

        f.lines.forEachIndexed { i, l ->
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    "${l.itemName} · ${l.lotNo} · ${l.line.qty.display()} ${l.uom} × ${l.line.unitPrice.display()} = ${l.lineTotal.toPlainString()}",
                    modifier = Modifier.weight(1f),
                )
                TextButton(onClick = { vm.removeLine(i) }) { Text("Sil") }
            }
        }
        if (f.lines.isNotEmpty()) Text("Toplam: ${ui.total.toPlainString()}", style = MaterialTheme.typography.titleMedium)
        Button(onClick = vm::confirm, enabled = f.customerId != null && f.lines.isNotEmpty(), modifier = Modifier.fillMaxWidth()) {
            Text("Onayla")
        }
        f.info?.let { Text(it, color = Color(0xFF2E7D32)) }
    }
}
