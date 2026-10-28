package com.avatar.lotsales.ui

import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.animateIntAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import com.avatar.lotsales.R
import com.avatar.lotsales.data.LotRepository
import com.avatar.lotsales.data.LotView
import com.avatar.lotsales.data.fefoOrder
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import java.time.LocalDate
import javax.inject.Inject

@HiltViewModel
class LotsViewModel @Inject constructor(repo: LotRepository) : ViewModel() {
    val query = MutableStateFlow("")
    val lots = combine(repo.observeLots(), query) { lots, q ->
        lots.filter { it.forSale && (q.isBlank() || it.lot.lotNo.contains(q, ignoreCase = true) || it.itemName.contains(q, ignoreCase = true)) }
            .sortedWith(compareBy<LotView> { it.itemName }.then(fefoOrder))
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), emptyList())
}

@Composable
fun LotsScreen(onSell: (String) -> Unit, vm: LotsViewModel = hiltViewModel()) {
    val query by vm.query.collectAsStateWithLifecycle()
    val lots by vm.lots.collectAsStateWithLifecycle()
    val firstFill = rememberFirstFill(lots.isNotEmpty())
    Column(Modifier.fillMaxSize()) {
        OutlinedTextField(
            value = query,
            onValueChange = { vm.query.value = it },
            placeholder = { Text(stringResource(R.string.lots_search)) },
            leadingIcon = { Icon(painterResource(R.drawable.ic_search), null) },
            singleLine = true,
            shape = MaterialTheme.shapes.extraLarge,
            colors = OutlinedTextFieldDefaults.colors(
                unfocusedContainerColor = MaterialTheme.colorScheme.surfaceContainerLowest,
                focusedContainerColor = MaterialTheme.colorScheme.surfaceContainerLowest,
                unfocusedBorderColor = MaterialTheme.colorScheme.outlineVariant,
            ),
            modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
        )
        LazyColumn(
            Modifier.fillMaxSize(),
            contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 4.dp, bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item(key = "summary") { LotSummary(lots) }
            if (lots.isEmpty()) item(key = "empty") { EmptyState(R.drawable.ic_inventory_2, stringResource(R.string.lots_empty), Modifier.animateItem()) }
            itemsIndexed(lots, key = { _, it -> it.lot.id }) { i, lot ->
                LotCard(lot, onClick = { onSell(lot.lot.id) }, Modifier.animateItem().staggeredEnter(i, firstFill))
            }
        }
    }
}

/** Aktif / bloke / SKT'si yakin lot sayilari; veri gelince 0'dan sayarak acilir. */
@Composable
private fun LotSummary(lots: List<LotView>) {
    val today = remember { LocalDate.now() }
    val active = lots.count { it.sellable }
    val expiring = lots.count { it.sellable && expiryOf(it.lot.expiryDate, today).urgent }
    Row(Modifier.padding(bottom = 4.dp), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
        SummaryTile(stringResource(R.string.status_active), active, StatusColors.success, Modifier.weight(1f))
        SummaryTile(stringResource(R.string.status_blocked), lots.size - active, StatusColors.danger, Modifier.weight(1f))
        SummaryTile(stringResource(R.string.summary_expiring), expiring, StatusColors.warning, Modifier.weight(1f))
    }
}

@Composable
private fun SummaryTile(label: String, count: Int, color: Color, modifier: Modifier) {
    val shown by animateIntAsState(count, tween(700, easing = FastOutSlowInEasing), label = "count")
    Column(modifier.background(color.copy(alpha = 0.10f), MaterialTheme.shapes.large).padding(horizontal = 14.dp, vertical = 12.dp)) {
        Text("$shown", style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.ExtraBold, color = color)
        Text(label, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/** Sunucunun lot durumu -> Turkce rozet; blocked_lots'ta olup durumu henuz guncellenmemis lot da "Bloke". */
private fun statusLabel(status: String) = when (status) {
    "recalled" -> R.string.status_recalled
    "consumed" -> R.string.status_consumed
    else -> R.string.status_blocked
}

@Composable
private fun LotCard(lot: LotView, onClick: () -> Unit, modifier: Modifier) {
    val shape = MaterialTheme.shapes.large
    var blockedTaps by remember { mutableIntStateOf(0) }
    Surface(
        shape = shape,
        color = if (lot.sellable) MaterialTheme.colorScheme.surfaceContainerLowest else StatusColors.dangerContainer,
        shadowElevation = if (lot.sellable) 1.dp else 0.dp,
        modifier = modifier.then(
            // bloke/geri cagrilmis lot tiklanamaz (semantik olarak da); dokunulursa yalniz sallanir
            if (lot.sellable) {
                Modifier.sharedCard("lot-${lot.lot.id}", shape).clip(shape).clickable(onClick = onClick)
            } else {
                Modifier.shake(blockedTaps.takeIf { it > 0 }).pointerInput(Unit) { detectTapGestures { blockedTaps++ } }
            },
        ),
    ) {
        Row(Modifier.padding(14.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            InitialsAvatar(lot.itemName)
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Text(lot.itemName, style = MaterialTheme.typography.titleMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(
                    stringResource(R.string.lot_row, lot.lot.lotNo, lot.available.display(), lot.baseUom),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                ExpiryBadge(lot.lot.expiryDate)
            }
            if (lot.sellable) {
                Chip(stringResource(R.string.status_active), StatusColors.success)
            } else {
                Chip(stringResource(statusLabel(lot.lot.status)), StatusColors.danger)
            }
        }
    }
}
