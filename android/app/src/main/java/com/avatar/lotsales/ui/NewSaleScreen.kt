package com.avatar.lotsales.ui

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.MutableTransitionState
import androidx.compose.animation.expandVertically
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.shrinkVertically
import androidx.compose.animation.slideInHorizontally
import androidx.compose.animation.slideOutHorizontally
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.navigation.toRoute
import com.avatar.lotsales.R
import com.avatar.lotsales.core.Decimals
import com.avatar.lotsales.data.DraftLine
import com.avatar.lotsales.data.LotRepository
import com.avatar.lotsales.data.LotView
import com.avatar.lotsales.data.SELLABLE_ITEM_TYPES
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
import kotlinx.coroutines.flow.map
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
    private val items = lotRepo.observeItems().map { all -> all.filter { it.itemType in SELLABLE_ITEM_TYPES } }
        .stateIn(viewModelScope, SharingStarted.Eagerly, emptyList())

    val ui = combine(form, lotRepo.observeCustomers(), items, lots) { f, customers, items, all ->
        NewSaleUi(f, customers, items, all.filter { it.forSale && it.lot.itemId == f.itemId }.sortedWith(fefoOrder))
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), NewSaleUi())

    /** Lot listesinden gelindiyse o lot; lot karti daha yuklenmeden secici kartla eslesir (sharedCard). */
    val initialLotId: String? = savedState.toRoute<NewSaleRoute>().lotId

    init {
        // Lot listesinden gelindiyse o lot (ve urunu) secili baslar.
        initialLotId?.let { lotId ->
            viewModelScope.launch {
                val lot = lots.first { l -> l.any { it.lot.id == lotId } }.first { it.lot.id == lotId }
                items.first { list -> list.any { it.id == lot.lot.itemId } } // varsayilan fiyat urunden gelir
                pickLot(lot)
            }
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
    val celebrate = rememberCelebration(f.info)
    // onaydan sonra toplam kutusu kapanirken 0.00'a yuvarlanmasin: son dolu tutar gosterilir
    val shownTotal = remember { mutableStateOf("") }
    if (f.lines.isNotEmpty()) shownTotal.value = ui.total.toPlainString()
    Box(Modifier.fillMaxSize()) {
        Column(
            Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            SectionCard {
                SectionLabel(stringResource(R.string.customer))
                Picker(stringResource(R.string.customer), ui.customer?.name, ui.customers, { it.name }, vm::selectCustomer)
                SectionLabel(stringResource(R.string.product))
                Picker(stringResource(R.string.product), ui.item?.name, ui.items, { it.name }, vm::selectItem)
                SectionLabel(stringResource(R.string.lot_fefo_label))
                val blockedTag = stringResource(R.string.blocked_tag)
                val fefoTag = stringResource(R.string.fefo_tag)
                Picker(
                    stringResource(R.string.lot),
                    ui.lot?.let { "${it.lot.lotNo} · ${it.available.display()} ${it.baseUom}" },
                    ui.itemLots,
                    { lot ->
                        buildString {
                            append("${lot.lot.lotNo} · SKT ${lot.lot.expiryDate ?: "—"} · ${lot.available.display()} ${lot.baseUom}")
                            if (!lot.sellable) append(" · $blockedTag")
                            if (lot == ui.fefoSuggestion) append(" · $fefoTag")
                        }
                    },
                    vm::pickLot,
                    // lot kartindan gelindiyse kart bu secicine donusur
                    Modifier.sharedCard("lot-${f.lotId ?: vm.initialLotId}", MaterialTheme.shapes.medium),
                )
                AnimatedVisibility(ui.lot != null) {
                    ui.lot?.let { ExpiryBadge(it.lot.expiryDate) }
                }
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    OutlinedTextField(
                        f.qty, vm::setQty, label = { Text(stringResource(R.string.qty)) }, singleLine = true, modifier = Modifier.weight(1f),
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
                    )
                    OutlinedTextField(
                        f.price, vm::setPrice, label = { Text(stringResource(R.string.unit_price)) }, singleLine = true, modifier = Modifier.weight(1f),
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
                    )
                }
                BouncyButton(onClick = vm::addLine, modifier = Modifier.fillMaxWidth(), tonal = true) {
                    Icon(painterResource(R.drawable.ic_add), null, Modifier.size(20.dp))
                    Text(stringResource(R.string.add_line), Modifier.padding(start = 8.dp))
                }
            }
            MessageBanner(f.error, isError = true)

            f.lines.forEach { l ->
                // ayni ornek form guncellemelerinde korunur; eklenen satir kayarak girer, silinen kayarak cikar
                key(System.identityHashCode(l)) {
                    val visible = remember { MutableTransitionState(false).apply { targetState = true } }
                    AnimatedVisibility(
                        visible,
                        enter = slideInHorizontally { it / 3 } + fadeIn() + expandVertically(),
                        exit = slideOutHorizontally { -it } + fadeOut() + shrinkVertically(),
                    ) {
                        DraftLineCard(l, onRemove = { visible.targetState = false })
                    }
                    if (visible.isIdle && !visible.currentState) LaunchedEffect(Unit) { vm.removeLine(f.lines.indexOf(l)) }
                }
            }
            AnimatedVisibility(f.lines.isNotEmpty(), enter = fadeIn() + expandVertically(), exit = fadeOut() + shrinkVertically()) {
                Row(
                    Modifier
                        .fillMaxWidth()
                        .background(MaterialTheme.colorScheme.primaryContainer, MaterialTheme.shapes.large)
                        .padding(horizontal = 18.dp, vertical = 14.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(
                        stringResource(R.string.total),
                        Modifier.weight(1f),
                        style = MaterialTheme.typography.titleMedium,
                        color = MaterialTheme.colorScheme.onPrimaryContainer,
                    )
                    RollingText(
                        shownTotal.value,
                        style = MaterialTheme.typography.headlineSmall.copy(fontWeight = FontWeight.ExtraBold),
                        color = MaterialTheme.colorScheme.onPrimaryContainer,
                    )
                }
            }
            BouncyButton(onClick = vm::confirm, enabled = f.customerId != null && f.lines.isNotEmpty(), modifier = Modifier.fillMaxWidth()) {
                Text(stringResource(R.string.confirm))
            }
            MessageBanner(f.info, isError = false)
        }
        SuccessCheck(celebrate, Modifier.align(Alignment.Center))
    }
}

@Composable
private fun DraftLineCard(l: DraftLineUi, onRemove: () -> Unit) {
    Surface(Modifier.fillMaxWidth(), shape = MaterialTheme.shapes.large, color = MaterialTheme.colorScheme.surfaceContainerLowest, shadowElevation = 1.dp) {
        Row(
            Modifier.padding(start = 14.dp, top = 10.dp, bottom = 10.dp, end = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            InitialsAvatar(l.itemName)
            Column(Modifier.weight(1f)) {
                Text(l.itemName, style = MaterialTheme.typography.titleSmall)
                Text(
                    "${l.lotNo} · ${l.line.qty.display()} ${l.uom} × ${l.line.unitPrice.display()}",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Text(l.lineTotal.toPlainString(), style = MaterialTheme.typography.titleSmall)
            IconButton(onClick = onRemove) {
                Icon(painterResource(R.drawable.ic_delete), stringResource(R.string.remove), tint = MaterialTheme.colorScheme.error)
            }
        }
    }
}
