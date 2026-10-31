package com.avatar.lotsales.ui

import androidx.annotation.DrawableRes
import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.animateIntAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
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
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import java.time.LocalDate
import javax.inject.Inject

@HiltViewModel
class LotsViewModel @Inject constructor(repo: LotRepository) : ViewModel() {
    val query = MutableStateFlow("")

    /** Ürün çipi; null = tümü. */
    val product = MutableStateFlow<String?>(null)

    /** Satış listesine giren tüm lotlar: özet kartları ve ürün çipleri bunu kullanır (arama/çipten bağımsız). */
    val all = repo.observeLots()
        .map { lots -> lots.filter { it.forSale }.sortedWith(compareBy<LotView> { it.itemName }.then(fefoOrder)) }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), emptyList())

    val lots = combine(all, query, product) { lots, q, p ->
        // seçili ürünün son lotu satılıp listeden düşerse filtre kendiliğinden "tümü"ne döner
        val name = p?.takeIf { n -> lots.any { it.itemName == n } }
        lots.filter {
            (name == null || it.itemName == name) &&
                (q.isBlank() || it.lot.lotNo.contains(q, ignoreCase = true) || it.itemName.contains(q, ignoreCase = true))
        }
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), emptyList())
}

/**
 * Stok ve lotlar (Stitch tasarımı): 2x2 özet kartı, senkron kartı, arama, ürün çipleri ve lot kartları.
 * Tasarımdaki barkod, raf konumu, kesim/menşei, "yeni lot girişi" gibi parçalar cihazda veri/yetki
 * olmadığı için yok; raf konumu yerine satış fiyatı gösterilir.
 */
@Composable
fun LotsScreen(
    onSell: (String) -> Unit,
    vm: LotsViewModel = hiltViewModel(),
    sync: SyncUi = SyncUi(),
    onRefresh: () -> Unit = {},
) {
    val query by vm.query.collectAsStateWithLifecycle()
    val product by vm.product.collectAsStateWithLifecycle()
    val all by vm.all.collectAsStateWithLifecycle()
    val lots by vm.lots.collectAsStateWithLifecycle()
    val today = remember { LocalDate.now() }
    val firstFill = rememberFirstFill(lots.isNotEmpty())
    LazyColumn(
        Modifier.fillMaxSize(),
        contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 8.dp, bottom = 24.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        item(key = "kpi") { KpiGrid(all, today) }
        item(key = "sync") { SyncCard(sync, onRefresh) }
        item(key = "search") { SearchField(query) { vm.query.value = it } }
        item(key = "chips") { ProductChips(all, product) { vm.product.value = it } }
        if (lots.isEmpty()) {
            item(key = "empty") {
                EmptyState(
                    R.drawable.ic_inventory_2,
                    stringResource(if (all.isEmpty()) R.string.lots_empty else R.string.lots_no_match),
                    Modifier.animateItem(),
                )
            }
        }
        itemsIndexed(lots, key = { _, it -> it.lot.id }) { i, lot ->
            LotCard(lot, today, onSell = { onSell(lot.lot.id) }, Modifier.animateItem().staggeredEnter(i, firstFill))
        }
    }
}

// ---------------------------------------------------------------------------
// Özet kartları
// ---------------------------------------------------------------------------

private data class Tone(val container: Color, val iconBg: Color, val iconFg: Color, val text: Color, val value: Color)

@Composable
private fun neutralTone() = MaterialTheme.colorScheme.run {
    Tone(surfaceContainerLowest, primaryContainer, onPrimaryContainer, onSurfaceVariant, onSurface)
}

@Composable
private fun warningTone() = MaterialTheme.colorScheme.run {
    Tone(tertiaryContainer, tertiary, onTertiary, onTertiaryContainer, onTertiaryContainer)
}

@Composable
private fun dangerTone() = MaterialTheme.colorScheme.run {
    Tone(errorContainer, error, onError, error, error)
}

/** Aktif / toplam stok / kritik SKT / bloke. Sayılar veri gelince 0'dan sayarak açılır. */
@Composable
private fun KpiGrid(lots: List<LotView>, today: LocalDate) {
    val sellable = lots.filter { it.sellable }
    val critical = sellable.count { expiryOf(it.lot.expiryDate, today).level <= ExpiryLevel.CRITICAL }
    val kg = sellable.filter { it.baseUom == "kg" }.sumOf { it.available }
    val pcs = sellable.filter { it.baseUom == "adet" }.sumOf { it.available }
    // kg varsa ana birim kg, adet ikinci satırda; yalnız adet varsa ana birim adet
    val (stock, stockUnit, stockSub) = when {
        kg.signum() > 0 -> Triple(kg.display(), "kg", if (pcs.signum() > 0) "+ ${pcs.display()} adet" else null)
        else -> Triple(pcs.display(), "adet", null)
    }
    val lotUnit = stringResource(R.string.kpi_unit_lots)
    Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
        Row(Modifier.height(IntrinsicSize.Min), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            KpiTile(
                stringResource(R.string.kpi_active), "${animatedCount(sellable.size)}", lotUnit,
                stringResource(R.string.kpi_active_sub, sellable.map { it.itemName }.distinct().size),
                R.drawable.ic_inventory_2, neutralTone(), Modifier.weight(1f),
            )
            KpiTile(
                stringResource(R.string.kpi_stock), stock, stockUnit, stockSub ?: stringResource(R.string.kpi_stock_sub),
                R.drawable.ic_weight, neutralTone(), Modifier.weight(1f),
            )
        }
        Row(Modifier.height(IntrinsicSize.Min), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            KpiTile(
                stringResource(R.string.kpi_critical), "${animatedCount(critical)}", lotUnit,
                stringResource(R.string.kpi_critical_sub, EXPIRY_CRITICAL_DAYS),
                R.drawable.ic_schedule, warningTone(), Modifier.weight(1f),
            )
            KpiTile(
                stringResource(R.string.status_blocked), "${animatedCount(lots.size - sellable.size)}", lotUnit,
                stringResource(R.string.kpi_blocked_sub), R.drawable.ic_lock, dangerTone(), Modifier.weight(1f),
            )
        }
    }
}

@Composable
private fun animatedCount(n: Int): Int = animateIntAsState(n, tween(700, easing = FastOutSlowInEasing), label = "count").value

@Composable
private fun KpiTile(title: String, value: String, unit: String, sub: String, @DrawableRes icon: Int, tone: Tone, modifier: Modifier) {
    Column(
        modifier
            .fillMaxHeight()
            .background(tone.container, MaterialTheme.shapes.large)
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Row(verticalAlignment = Alignment.Top) {
            Text(title, style = MaterialTheme.typography.labelLarge, color = tone.text, modifier = Modifier.weight(1f))
            Box(Modifier.size(32.dp).background(tone.iconBg, RoundedCornerShape(10.dp)), contentAlignment = Alignment.Center) {
                Icon(painterResource(icon), null, Modifier.size(18.dp), tint = tone.iconFg)
            }
        }
        Spacer(Modifier.weight(1f))
        Row(verticalAlignment = Alignment.Bottom) {
            RollingText(value, style = MaterialTheme.typography.headlineMedium.copy(fontWeight = FontWeight.ExtraBold), color = tone.value)
            Text(unit, style = MaterialTheme.typography.bodyMedium, color = tone.text, modifier = Modifier.padding(start = 4.dp, bottom = 4.dp))
        }
        Text(sub, style = MaterialTheme.typography.bodySmall, color = tone.text)
    }
}

// ---------------------------------------------------------------------------
// Senkron kartı, arama, ürün çipleri
// ---------------------------------------------------------------------------

/** Bekleyen kayıt / son senkron; "Yenile" bir senkron turu başlatır (çevrimdışıysa bağlantı gelince çalışır). */
@Composable
private fun SyncCard(sync: SyncUi, onRefresh: () -> Unit) {
    val pending = sync.status.pending
    val dot by animateColorAsState(
        when {
            !sync.online -> StatusColors.neutral
            pending > 0 -> StatusColors.warning
            else -> StatusColors.success
        },
        label = "syncDot",
    )
    val subtitle = when {
        !sync.online -> stringResource(R.string.sync_card_offline)
        sync.status.lastSyncAt != null -> stringResource(R.string.sync_card_last, formatInstant(sync.status.lastSyncAt))
        else -> stringResource(R.string.sync_card_never)
    }
    Row(
        Modifier
            .fillMaxWidth()
            .background(MaterialTheme.colorScheme.surfaceContainerLow, MaterialTheme.shapes.large)
            .padding(12.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Box(
            Modifier.size(40.dp).background(MaterialTheme.colorScheme.primaryContainer, RoundedCornerShape(12.dp)),
            contentAlignment = Alignment.Center,
        ) {
            Icon(painterResource(R.drawable.ic_cloud_sync), null, Modifier.size(22.dp), tint = MaterialTheme.colorScheme.onPrimaryContainer)
        }
        Column(Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                AnimatedContent(pending, Modifier.weight(1f, fill = false), label = "syncTitle") { n ->
                    Text(
                        if (n > 0) stringResource(R.string.sync_card_pending, n) else stringResource(R.string.sync_card_ok),
                        style = MaterialTheme.typography.titleSmall,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
                Box(Modifier.size(8.dp).background(dot, CircleShape))
            }
            Text(subtitle, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        OutlinedButton(
            onClick = onRefresh,
            enabled = !sync.syncing,
            contentPadding = PaddingValues(horizontal = 12.dp, vertical = 6.dp),
            border = BorderStroke(1.dp, MaterialTheme.colorScheme.outlineVariant),
            colors = ButtonDefaults.outlinedButtonColors(containerColor = MaterialTheme.colorScheme.surfaceContainerLowest),
        ) {
            Icon(painterResource(R.drawable.ic_sync), null, Modifier.size(16.dp).then(if (sync.syncing) Modifier.spin() else Modifier))
            Spacer(Modifier.width(6.dp))
            Text(stringResource(R.string.refresh))
        }
    }
}

@Composable
private fun SearchField(query: String, onChange: (String) -> Unit) {
    OutlinedTextField(
        value = query,
        onValueChange = onChange,
        placeholder = { Text(stringResource(R.string.lots_search)) },
        leadingIcon = { Icon(painterResource(R.drawable.ic_search), null) },
        singleLine = true,
        shape = MaterialTheme.shapes.extraLarge,
        colors = OutlinedTextFieldDefaults.colors(
            unfocusedContainerColor = MaterialTheme.colorScheme.surfaceContainerLowest,
            focusedContainerColor = MaterialTheme.colorScheme.surfaceContainerLowest,
            unfocusedBorderColor = MaterialTheme.colorScheme.outlineVariant,
        ),
        modifier = Modifier.fillMaxWidth(),
    )
}

/** Ürüne göre süzme; tek ürün varsa gizli (süzecek bir şey yok). */
@Composable
private fun ProductChips(lots: List<LotView>, selected: String?, onSelect: (String?) -> Unit) {
    val counts = remember(lots) { lots.groupingBy { it.itemName }.eachCount().toSortedMap() }
    if (counts.size < 2) return
    val current = selected?.takeIf { it in counts }
    LazyRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        item(key = "all") { ProductChip(stringResource(R.string.chip_all), lots.size, current == null) { onSelect(null) } }
        items(counts.entries.toList(), key = { "p:${it.key}" }) { (name, n) -> ProductChip(name, n, current == name) { onSelect(name) } }
    }
}

@Composable
private fun ProductChip(label: String, count: Int, selected: Boolean, onClick: () -> Unit) {
    val c = MaterialTheme.colorScheme
    val bg by animateColorAsState(if (selected) c.primary else c.surfaceContainerLowest, label = "chipBg")
    val fg by animateColorAsState(if (selected) c.onPrimary else c.onSurface, label = "chipFg")
    Surface(
        onClick = onClick,
        shape = CircleShape,
        color = bg,
        border = if (selected) null else BorderStroke(1.dp, c.outlineVariant),
    ) {
        Row(Modifier.padding(horizontal = 14.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(label, style = MaterialTheme.typography.labelLarge, color = fg)
            Text(
                "$count",
                style = MaterialTheme.typography.labelSmall,
                color = fg,
                modifier = Modifier.background(fg.copy(alpha = 0.14f), CircleShape).padding(horizontal = 7.dp, vertical = 1.dp),
            )
        }
    }
}

// ---------------------------------------------------------------------------
// Lot kartı
// ---------------------------------------------------------------------------

/** Sunucunun lot durumu -> Türkçe rozet; blocked_lots'ta olup durumu henüz güncellenmemiş lot da "Bloke". */
private fun statusLabel(status: String) = when (status) {
    "recalled" -> R.string.status_recalled
    "consumed" -> R.string.status_consumed
    else -> R.string.status_blocked
}

@Composable
private fun LotCard(lot: LotView, today: LocalDate, onSell: () -> Unit, modifier: Modifier) {
    val c = MaterialTheme.colorScheme
    val expiry = remember(lot.lot.expiryDate, today) { expiryOf(lot.lot.expiryDate, today) }
    val critical = lot.sellable && expiry.level <= ExpiryLevel.CRITICAL
    val shape = MaterialTheme.shapes.large
    var blockedTaps by remember { mutableIntStateOf(0) }
    Surface(
        shape = shape,
        color = c.surfaceContainerLowest,
        shadowElevation = if (lot.sellable) 1.dp else 0.dp,
        border = if (lot.sellable) null else BorderStroke(1.dp, c.error.copy(alpha = 0.3f)),
        modifier = modifier.then(
            // bloke/geri çağrılmış lot tıklanamaz (semantik olarak da); dokunulursa yalnız sallanır
            if (lot.sellable) {
                Modifier.sharedCard("lot-${lot.lot.id}", shape).clip(shape).clickable(onClick = onSell)
            } else {
                Modifier.shake(blockedTaps.takeIf { it > 0 }).pointerInput(Unit) { detectTapGestures { blockedTaps++ } }
            },
        ),
    ) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Row(verticalAlignment = Alignment.Top, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                if (lot.sellable) {
                    InitialsAvatar(lot.itemName, shape = RoundedCornerShape(12.dp))
                } else {
                    Box(Modifier.size(44.dp).background(c.errorContainer, RoundedCornerShape(12.dp)), contentAlignment = Alignment.Center) {
                        Icon(painterResource(R.drawable.ic_lock), null, Modifier.size(22.dp), tint = c.error)
                    }
                }
                // rozet yalniz ad satirinda: lot no alt satirin tamamini kullanir, ortasindan bolunmez
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    Row(verticalAlignment = Alignment.Top, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        Text(
                            lot.itemName,
                            style = MaterialTheme.typography.titleMedium,
                            maxLines = 2,
                            overflow = TextOverflow.Ellipsis,
                            modifier = Modifier.weight(1f),
                        )
                        StatusBadge(lot, expiry)
                    }
                    Text(lot.lot.lotNo, style = MaterialTheme.typography.labelLarge, color = c.primary)
                }
            }

            val infoBg = when {
                !lot.sellable -> StatusColors.dangerContainer
                critical -> c.tertiaryContainer.copy(alpha = 0.55f)
                else -> c.surfaceContainerLow
            }
            val expiryColor = when (expiry.level) {
                ExpiryLevel.EXPIRED -> c.error
                ExpiryLevel.CRITICAL -> c.tertiary
                ExpiryLevel.SOON -> StatusColors.warning
                else -> c.onSurface
            }
            Row(Modifier.fillMaxWidth().background(infoBg, MaterialTheme.shapes.medium).padding(vertical = 10.dp, horizontal = 8.dp)) {
                InfoCell(stringResource(R.string.lot_remaining), "${lot.available.display()} ${lot.baseUom}", Modifier.weight(1f))
                InfoCell(stringResource(R.string.lot_price), lot.salePrice?.let { "${it.display()} €" } ?: "—", Modifier.weight(1f))
                InfoCell(stringResource(R.string.lot_expiry), expiry.date?.let(expiryDateFormat::format) ?: "—", Modifier.weight(1f), expiryColor)
            }

            if (critical) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    Icon(painterResource(R.drawable.ic_warning), null, Modifier.size(16.dp), tint = c.tertiary)
                    Text(stringResource(R.string.lot_fefo_note), style = MaterialTheme.typography.bodySmall, color = c.tertiary, fontWeight = FontWeight.SemiBold)
                }
            }

            if (lot.sellable) {
                BouncyButton(
                    onClick = onSell,
                    modifier = Modifier.fillMaxWidth(),
                    colors = if (critical) ButtonDefaults.buttonColors(containerColor = c.tertiary, contentColor = c.onTertiary) else null,
                ) {
                    Icon(painterResource(R.drawable.ic_shopping_cart), null, Modifier.size(18.dp))
                    Spacer(Modifier.width(8.dp))
                    Text(stringResource(if (critical) R.string.lot_sell_priority else R.string.lot_sell))
                }
            } else {
                Column(
                    Modifier.fillMaxWidth().background(c.errorContainer, MaterialTheme.shapes.medium).padding(12.dp),
                    verticalArrangement = Arrangement.spacedBy(4.dp),
                ) {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                        Icon(painterResource(R.drawable.ic_warning), null, Modifier.size(16.dp), tint = c.error)
                        Text(
                            stringResource(if (lot.lot.status == "recalled") R.string.lot_recalled_title else R.string.lot_blocked_title),
                            style = MaterialTheme.typography.labelLarge,
                            color = c.error,
                        )
                    }
                    Text(lot.blockedReason ?: stringResource(R.string.lot_blocked_default), style = MaterialTheme.typography.bodySmall, color = c.onErrorContainer)
                }
            }
        }
    }
}

@Composable
private fun InfoCell(label: String, value: String, modifier: Modifier, valueColor: Color = MaterialTheme.colorScheme.onSurface) {
    Column(modifier, horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Text(label, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Text(value, style = MaterialTheme.typography.titleSmall, color = valueColor, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

/** Aktif (yeşil) / kritik SKT (turuncu) / SKT geçti (kırmızı) / bloke-geri çağrıldı (kırmızı dolu). */
@Composable
private fun StatusBadge(lot: LotView, expiry: Expiry) {
    if (!lot.sellable) {
        Row(
            Modifier.background(MaterialTheme.colorScheme.error, CircleShape).padding(horizontal = 10.dp, vertical = 5.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            Icon(painterResource(R.drawable.ic_lock), null, Modifier.size(12.dp), tint = MaterialTheme.colorScheme.onError)
            Text(stringResource(statusLabel(lot.lot.status)), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onError)
        }
        return
    }
    when (expiry.level) {
        ExpiryLevel.EXPIRED -> Chip(stringResource(R.string.expiry_expired), StatusColors.danger, R.drawable.ic_warning)
        ExpiryLevel.CRITICAL -> Chip(
            if (expiry.days == 0L) stringResource(R.string.expiry_today) else stringResource(R.string.badge_critical, expiry.days!!),
            MaterialTheme.colorScheme.tertiary,
            R.drawable.ic_warning,
        )
        else -> Chip(stringResource(R.string.status_active), StatusColors.success)
    }
}
