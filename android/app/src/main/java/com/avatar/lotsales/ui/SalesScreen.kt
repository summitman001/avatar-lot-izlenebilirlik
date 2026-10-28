package com.avatar.lotsales.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
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
    private val orderId = savedState.toRoute<SaleDetailRoute>().orderId
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
    if (sales.isEmpty()) Text(stringResource(R.string.sales_empty), Modifier.padding(16.dp))
    LazyColumn(Modifier.fillMaxSize()) {
        items(sales, key = { it.id }) { sale ->
            ListItem(
                headlineContent = { Text("${sale.orderNo} · ${sale.customerName.orEmpty()}") },
                supportingContent = { Text("${formatInstant(sale.soldAt)} · ${stringResource(R.string.total)}: ${sale.totalAmount.toPlainString()}") },
                trailingContent = { SaleStatusChip(sale) },
                modifier = Modifier.clickable { onOpen(sale.id) },
            )
            HorizontalDivider()
        }
    }
}

@Composable
private fun SaleStatusChip(sale: SaleSummary) = when {
    sale.status == OrderStatus.REJECTED -> Chip(stringResource(R.string.status_rejected), MaterialTheme.colorScheme.error)
    sale.status == OrderStatus.FLAGGED -> Chip(stringResource(R.string.status_flagged), Color(0xFFF57C00))
    sale.synced -> Chip(stringResource(R.string.status_synced), Color(0xFF2E7D32))
    else -> Chip(stringResource(R.string.status_pending), Color(0xFF757575))
}

@Composable
fun SaleDetailScreen(vm: SaleDetailViewModel = hiltViewModel()) {
    val ui by vm.ui.collectAsStateWithLifecycle()
    var returning by remember { mutableStateOf<LineWithReturns?>(null) }
    val sale = ui.sale ?: return
    Column(Modifier.fillMaxSize().padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text("${sale.orderNo} · ${sale.customerName.orEmpty()}", style = MaterialTheme.typography.titleMedium, modifier = Modifier.weight(1f))
            SaleStatusChip(sale)
        }
        Text("${formatInstant(sale.soldAt)} · ${stringResource(R.string.total)}: ${sale.totalAmount.toPlainString()}")
        ui.error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
        ui.info?.let { Text(it, color = Color(0xFF2E7D32)) }
        LazyColumn {
            items(ui.lines, key = { it.line.id }) { l ->
                ListItem(
                    headlineContent = { Text("${l.line.itemName.orEmpty()} · ${stringResource(R.string.lot)} ${l.line.lotNo.orEmpty()}") },
                    supportingContent = {
                        Text(
                            "${l.line.qty.display()} × ${l.line.unitPrice.display()} = ${l.line.lineTotal.toPlainString()}" +
                                if (l.returned.signum() > 0) " · ${stringResource(R.string.returned)}: ${l.returned.display()}" else "",
                        )
                    },
                    trailingContent = {
                        if (sale.status != OrderStatus.REJECTED && l.returnable.signum() > 0) {
                            TextButton(onClick = { returning = l }) { Text(stringResource(R.string.take_return)) }
                        }
                    },
                )
                HorizontalDivider()
            }
        }
    }
    returning?.let { line -> ReturnDialog(line, onDismiss = { returning = null }, onConfirm = { q, r -> vm.createReturn(line.line.id, q, r) { returning = null } }) }
}

@Composable
private fun ReturnDialog(line: LineWithReturns, onDismiss: () -> Unit, onConfirm: (String, String) -> Unit) {
    var qty by remember { mutableStateOf(line.returnable.display()) }
    var reason by remember { mutableStateOf("") }
    AlertDialog(
        onDismissRequest = onDismiss,
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
    )
}
