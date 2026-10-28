package com.avatar.lotsales.ui

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.navigation.toRoute
import com.avatar.lotsales.R
import com.avatar.lotsales.core.Decimals
import com.avatar.lotsales.data.SalesRepository
import com.avatar.lotsales.data.db.OrderStatus
import com.avatar.lotsales.data.db.SaleLineView
import com.avatar.lotsales.data.db.SaleSummary
import com.avatar.lotsales.domain.SaleException
import com.avatar.lotsales.sync.SyncScheduler
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import java.math.BigDecimal
import javax.inject.Inject

@HiltViewModel
class SalesViewModel @Inject constructor(repo: SalesRepository) : ViewModel() {
    val sales = repo.observeSales().stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), emptyList())
}

data class LineWithReturns(val line: SaleLineView, val returned: BigDecimal) {
    val returnable: BigDecimal get() = line.qty - returned
}

data class SaleDetailUi(
    val sale: SaleSummary? = null,
    val lines: List<LineWithReturns> = emptyList(),
    val error: String? = null,
    val info: String? = null,
)

@HiltViewModel
class SaleDetailViewModel @Inject constructor(
    savedState: SavedStateHandle,
    private val repo: SalesRepository,
    private val scheduler: SyncScheduler,
) : ViewModel() {
    val orderId = savedState.toRoute<SaleDetailRoute>().orderId
    private val message = MutableStateFlow<Pair<String?, String?>>(null to null) // error to info

    val ui = combine(repo.observeSale(orderId), repo.observeLines(orderId), repo.observeReturns(orderId), message) { sale, lines, returns, msg ->
        val returned = returns.groupBy { it.salesLineId }.mapValues { (_, rs) -> rs.fold(BigDecimal.ZERO) { a, r -> a + r.qty } }
        SaleDetailUi(sale, lines.map { LineWithReturns(it, returned[it.id] ?: BigDecimal.ZERO) }, msg.first, msg.second)
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), SaleDetailUi())

    /** onDone: iade kaydedilince cagrilir (dialog kapanir); hata varsa dialog acik kalir. */
    fun createReturn(lineId: String, qtyText: String, reason: String, onDone: () -> Unit) {
        viewModelScope.launch {
            try {
                repo.createReturn(lineId, Decimals.parse(qtyText, Decimals.QTY_SCALE), reason)
                message.value = null to "İade kaydedildi (senkron kuyruğunda)"
                scheduler.syncNow()
                onDone()
            } catch (e: SaleException) {
                message.value = e.error.message to null
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                message.value = "İade kaydedilemedi: ${e.message}" to null // transaction geri alindi
            }
        }
    }
}

@Composable
fun SalesScreen(onOpen: (String) -> Unit, vm: SalesViewModel = hiltViewModel()) {
    val sales by vm.sales.collectAsStateWithLifecycle()
    val firstFill = rememberFirstFill(sales.isNotEmpty())
    LazyColumn(
        Modifier.fillMaxSize(),
        contentPadding = PaddingValues(16.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        if (sales.isEmpty()) item(key = "empty") { EmptyState(R.drawable.ic_receipt_long, stringResource(R.string.sales_empty), Modifier.animateItem()) }
        itemsIndexed(sales, key = { _, it -> it.id }) { i, sale ->
            SaleCard(sale, onClick = { onOpen(sale.id) }, Modifier.animateItem().staggeredEnter(i, firstFill))
        }
    }
}

@Composable
private fun SaleCard(sale: SaleSummary, onClick: () -> Unit, modifier: Modifier) {
    val shape = MaterialTheme.shapes.large
    Surface(
        shape = shape,
        color = MaterialTheme.colorScheme.surfaceContainerLowest,
        shadowElevation = 1.dp,
        modifier = modifier.sharedCard("sale-${sale.id}", shape).clip(shape).clickable(onClick = onClick),
    ) {
        Row(Modifier.padding(14.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            InitialsAvatar(sale.customerName.orEmpty())
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text(sale.customerName.orEmpty(), style = MaterialTheme.typography.titleMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(
                    "${sale.orderNo} · ${formatInstant(sale.soldAt)}",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Column(horizontalAlignment = Alignment.End, verticalArrangement = Arrangement.spacedBy(6.dp)) {
                Text(sale.totalAmount.toPlainString(), style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
                SaleStatusChip(sale)
            }
        }
    }
}

@Composable
private fun SaleStatusChip(sale: SaleSummary) = when {
    sale.status == OrderStatus.REJECTED -> Chip(stringResource(R.string.status_rejected), StatusColors.danger, R.drawable.ic_warning)
    sale.status == OrderStatus.FLAGGED -> Chip(stringResource(R.string.status_flagged), StatusColors.warning, R.drawable.ic_warning)
    sale.synced -> Chip(stringResource(R.string.status_synced), StatusColors.success, R.drawable.ic_cloud_done)
    else -> Chip(stringResource(R.string.status_pending), StatusColors.neutral, R.drawable.ic_schedule)
}

@Composable
fun SaleDetailScreen(vm: SaleDetailViewModel = hiltViewModel()) {
    val ui by vm.ui.collectAsStateWithLifecycle()
    var returning by remember { mutableStateOf<LineWithReturns?>(null) }
    var returnsSaved by remember { mutableIntStateOf(0) }
    val celebrate = rememberCelebration(returnsSaved.takeIf { it > 0 })
    val sale = ui.sale
    Box(Modifier.fillMaxSize()) {
        LazyColumn(
            Modifier.fillMaxSize(),
            contentPadding = PaddingValues(16.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            // baslik karti satis yuklenmeden de cizilir: listedeki kart buna donusur (key rotadan)
            item(key = "header") { SaleHeader(vm.orderId, sale) }
            item(key = "messages") {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    MessageBanner(ui.error, isError = true)
                    MessageBanner(ui.info, isError = false)
                }
            }
            items(ui.lines, key = { it.line.id }) { l ->
                SaleLineCard(l, canReturn = sale != null && sale.status != OrderStatus.REJECTED, onReturn = { returning = l }, Modifier.animateItem())
            }
        }
        SuccessCheck(celebrate, Modifier.align(Alignment.Center), diameter = 88.dp)
    }
    returning?.let { line ->
        ReturnDialog(
            line,
            onDismiss = { returning = null },
            onConfirm = { q, r ->
                vm.createReturn(line.line.id, q, r) {
                    returning = null
                    returnsSaved++
                }
            },
        )
    }
}

@Composable
private fun SaleHeader(orderId: String, sale: SaleSummary?) {
    val shape = MaterialTheme.shapes.large
    Surface(
        Modifier.fillMaxWidth().sharedCard("sale-$orderId", shape),
        shape = shape,
        color = MaterialTheme.colorScheme.primaryContainer,
    ) {
        Column(Modifier.padding(18.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                InitialsAvatar(sale?.customerName.orEmpty(), Modifier.alpha(if (sale == null) 0f else 1f)) // yuklenirken "?" gorunmesin
                Column(Modifier.weight(1f)) {
                    Text(sale?.customerName.orEmpty(), style = MaterialTheme.typography.titleLarge, color = MaterialTheme.colorScheme.onPrimaryContainer)
                    Text(
                        sale?.let { "${it.orderNo} · ${formatInstant(it.soldAt)}" }.orEmpty(),
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onPrimaryContainer,
                    )
                }
                sale?.let { SaleStatusChip(it) }
            }
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    stringResource(R.string.total),
                    Modifier.weight(1f),
                    style = MaterialTheme.typography.titleMedium,
                    color = MaterialTheme.colorScheme.onPrimaryContainer,
                )
                RollingText(
                    sale?.totalAmount?.toPlainString().orEmpty(),
                    style = MaterialTheme.typography.headlineSmall.copy(fontWeight = FontWeight.ExtraBold),
                    color = MaterialTheme.colorScheme.onPrimaryContainer,
                )
            }
        }
    }
}

@Composable
private fun SaleLineCard(l: LineWithReturns, canReturn: Boolean, onReturn: () -> Unit, modifier: Modifier) {
    Surface(modifier.fillMaxWidth(), shape = MaterialTheme.shapes.large, color = MaterialTheme.colorScheme.surfaceContainerLowest, shadowElevation = 1.dp) {
        Row(Modifier.padding(14.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            InitialsAvatar(l.line.itemName.orEmpty())
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Text(l.line.itemName.orEmpty(), style = MaterialTheme.typography.titleSmall)
                Text(
                    "${stringResource(R.string.lot)} ${l.line.lotNo.orEmpty()} · ${l.line.qty.display()} × ${l.line.unitPrice.display()} = ${l.line.lineTotal.toPlainString()}",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                AnimatedVisibility(l.returned.signum() > 0) {
                    Chip("${stringResource(R.string.returned)}: ${l.returned.display()}", StatusColors.warning, R.drawable.ic_assignment_return)
                }
            }
            AnimatedVisibility(canReturn && l.returnable.signum() > 0) {
                BouncyButton(onClick = onReturn, tonal = true) { Text(stringResource(R.string.take_return)) }
            }
        }
    }
}

@Composable
private fun ReturnDialog(line: LineWithReturns, onDismiss: () -> Unit, onConfirm: (String, String) -> Unit) {
    var qty by remember { mutableStateOf(line.returnable.display()) }
    var reason by remember { mutableStateOf("") }
    AlertDialog(
        onDismissRequest = onDismiss,
        icon = { Icon(painterResource(R.drawable.ic_assignment_return), null, Modifier.size(28.dp)) },
        title = { Text(stringResource(R.string.return_title, line.line.lotNo.orEmpty())) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(stringResource(R.string.return_max, line.returnable.display()))
                OutlinedTextField(
                    qty, { qty = it }, label = { Text(stringResource(R.string.qty)) }, singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
                )
                OutlinedTextField(reason, { reason = it }, label = { Text(stringResource(R.string.return_reason)) })
            }
        },
        confirmButton = { TextButton(onClick = { onConfirm(qty, reason) }) { Text(stringResource(R.string.save)) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.cancel)) } },
        containerColor = MaterialTheme.colorScheme.surfaceContainerLowest,
    )
}
