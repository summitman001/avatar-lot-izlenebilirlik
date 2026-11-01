package com.avatar.lotsales.ui

import androidx.annotation.DrawableRes
import androidx.annotation.StringRes
import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.animateIntAsState
import androidx.compose.animation.core.tween
import androidx.compose.animation.expandVertically
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.shrinkVertically
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
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
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
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
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
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
import java.math.BigDecimal
import java.math.RoundingMode
import java.time.LocalDate
import javax.inject.Inject

/** Liste çipleri: tümü / kritik SKT / bloke. Sayımlar özet kartlarıyla aynı kuralı kullanır. */
enum class LotFilter(@StringRes val label: Int) {
    ALL(R.string.chip_all),
    CRITICAL(R.string.kpi_critical),
    BLOCKED(R.string.status_blocked),
    ;

    fun matches(lot: LotView, today: LocalDate) = when (this) {
        ALL -> true
        CRITICAL -> lot.isCritical(today)
        BLOCKED -> !lot.sellable
    }
}

/** Kritik SKT: satılabilir ve SKT'ye en fazla EXPIRY_CRITICAL_DAYS gün (geçmiş dahil). */
private fun LotView.isCritical(today: LocalDate) = sellable && expiryOf(lot.expiryDate, today).level <= ExpiryLevel.CRITICAL

@HiltViewModel
class LotsViewModel @Inject constructor(repo: LotRepository) : ViewModel() {
    val query = MutableStateFlow("")
    val filter = MutableStateFlow(LotFilter.ALL)

    /** Satış listesine giren tüm lotlar, FEFO sırasında: özet kartları ve çip sayıları bunu kullanır (arama/çipten bağımsız). */
    val all = repo.observeLots()
        .map { lots -> lots.filter { it.forSale }.sortedWith(fefoOrder) }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), emptyList())

    val lots = combine(all, query, filter) { lots, q, f ->
        val today = LocalDate.now()
        lots.filter {
            f.matches(it, today) &&
                (q.isBlank() || it.lot.lotNo.contains(q, ignoreCase = true) || it.itemName.contains(q, ignoreCase = true))
        }
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), emptyList())
}

// Tasarım A: kart köşeleri (özet 18, liste satırı 16).
private val TileShape = RoundedCornerShape(18.dp)
private val RowShape = RoundedCornerShape(16.dp)

/**
 * Lotlar sekmesinin ilk ekranı: 2x2 özet kartı, senkron kartı ve lot listesini açan buton. Lot ayrıntıları
 * yalnız istenirse ([LotListScreen]) görünür; en acil lot butonun altında tek satırla hatırlatılır.
 */
@Composable
fun LotsScreen(
    onOpenList: () -> Unit,
    vm: LotsViewModel = hiltViewModel(),
    sync: SyncUi = SyncUi(),
    onRefresh: () -> Unit = {},
) {
    val all by vm.all.collectAsStateWithLifecycle()
    val today = remember { LocalDate.now() }
    Column(
        Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(start = 16.dp, end = 16.dp, top = 8.dp, bottom = 24.dp),
        verticalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        KpiGrid(all, today)
        SyncCard(sync, onRefresh)
        if (all.isEmpty()) {
            EmptyState(R.drawable.ic_inventory_2, stringResource(R.string.lots_empty))
        } else {
            OpenListButton(all.size, onOpenList)
            MostUrgentHint(all, today)
        }
    }
}

/**
 * Lot listesi: arama, durum çipleri ve sade satırlar (ürün, lot no, kalan, durum). Satıra dokununca ayrıntılar
 * aynı kartın içinde açılır (aynı anda tek satır); satış butonu yalnız satılabilir lotta görünür.
 * Tasarımdaki giriş tarihi, kaynak ve hareketler cihazda veri olmadığı için yok.
 */
@Composable
fun LotListScreen(
    onSell: (String) -> Unit,
    vm: LotsViewModel = hiltViewModel(),
    warehouse: String? = null,
) {
    val query by vm.query.collectAsStateWithLifecycle()
    val filter by vm.filter.collectAsStateWithLifecycle()
    val all by vm.all.collectAsStateWithLifecycle()
    val lots by vm.lots.collectAsStateWithLifecycle()
    val today = remember { LocalDate.now() }
    val firstFill = rememberFirstFill(lots.isNotEmpty())
    var expanded by rememberSaveable { mutableStateOf<String?>(null) }
    Column(Modifier.fillMaxSize()) {
        Column(
            Modifier.padding(start = 16.dp, end = 16.dp, top = 8.dp, bottom = 4.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            SearchField(query) { vm.query.value = it }
            FilterChips(all, today, filter) {
                vm.filter.value = it
                expanded = null
            }
        }
        LazyColumn(
            Modifier.fillMaxSize(),
            contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 8.dp, bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
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
                LotRow(
                    lot,
                    today,
                    warehouse,
                    expanded = expanded == lot.lot.id,
                    onToggle = { expanded = if (expanded == lot.lot.id) null else lot.lot.id },
                    onSell = { onSell(lot.lot.id) },
                    modifier = Modifier.animateItem().staggeredEnter(i, firstFill),
                )
            }
        }
    }
}

// ---------------------------------------------------------------------------
// Özet kartları
// ---------------------------------------------------------------------------

private data class Tone(val container: Color, val border: Color, val iconBg: Color, val iconFg: Color, val text: Color, val value: Color, val sub: Color)

@Composable
private fun neutralTone() = MaterialTheme.colorScheme.run {
    Tone(surfaceContainerLowest, StatusColors.cardBorder, primaryContainer, primary, onSurfaceVariant, onSurface, StatusColors.neutral)
}

@Composable
private fun warningTone() = MaterialTheme.colorScheme.run {
    Tone(tertiaryContainer, StatusColors.criticalBorder, tertiary, onTertiary, onTertiaryContainer, onTertiaryContainer, onTertiaryContainer)
}

@Composable
private fun dangerTone() = MaterialTheme.colorScheme.run {
    Tone(errorContainer, StatusColors.dangerBorder, error, onError, onErrorContainer, onErrorContainer, onErrorContainer)
}

/** Aktif / toplam stok / kritik SKT / bloke. Sayılar veri gelince 0'dan sayarak açılır. */
@Composable
private fun KpiGrid(lots: List<LotView>, today: LocalDate) {
    val sellable = lots.filter { it.sellable }
    val critical = sellable.count { it.isCritical(today) }
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
            .background(tone.container, TileShape)
            .border(1.dp, tone.border, TileShape)
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(title, style = MaterialTheme.typography.labelLarge, color = tone.text, modifier = Modifier.weight(1f))
            Box(Modifier.size(32.dp).background(tone.iconBg, RoundedCornerShape(10.dp)), contentAlignment = Alignment.Center) {
                Icon(painterResource(icon), null, Modifier.size(18.dp), tint = tone.iconFg)
            }
        }
        Spacer(Modifier.weight(1f))
        Row(verticalAlignment = Alignment.Bottom) {
            RollingText(value, style = MaterialTheme.typography.headlineLarge.copy(fontWeight = FontWeight.ExtraBold), color = tone.value)
            Text(unit, style = MaterialTheme.typography.bodyMedium, color = tone.text, modifier = Modifier.padding(start = 4.dp, bottom = 6.dp))
        }
        Text(sub, style = MaterialTheme.typography.bodySmall, color = tone.sub)
    }
}

// ---------------------------------------------------------------------------
// Senkron kartı, liste butonu, en acil lot
// ---------------------------------------------------------------------------

/** Bekleyen kayıt / son senkron; "Yenile" bir senkron turu başlatır (çevrimdışıysa bağlantı gelince çalışır). */
@Composable
private fun SyncCard(sync: SyncUi, onRefresh: () -> Unit) {
    val c = MaterialTheme.colorScheme
    val pending = sync.status.pending
    // ikon kutusunun rengi durumu söyler: güncel yeşil, bekleyen turuncu, çevrimdışı gri
    val (bgTarget, fgTarget) = when {
        !sync.online -> c.surfaceContainer to StatusColors.neutral
        pending > 0 -> c.tertiaryContainer to c.tertiary
        else -> StatusColors.successContainer to StatusColors.success
    }
    val iconBg by animateColorAsState(bgTarget, label = "syncBg")
    val iconFg by animateColorAsState(fgTarget, label = "syncFg")
    val subtitle = when {
        !sync.online -> stringResource(R.string.sync_card_offline)
        sync.status.lastSyncAt != null -> stringResource(R.string.sync_card_last, formatInstant(sync.status.lastSyncAt))
        else -> stringResource(R.string.sync_card_never)
    }
    Row(
        Modifier
            .fillMaxWidth()
            .background(c.surfaceContainerLowest, TileShape)
            .border(1.dp, StatusColors.cardBorder, TileShape)
            .padding(horizontal = 16.dp, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Box(Modifier.size(40.dp).background(iconBg, RoundedCornerShape(12.dp)), contentAlignment = Alignment.Center) {
            Icon(painterResource(R.drawable.ic_cloud_sync), null, Modifier.size(22.dp), tint = iconFg)
        }
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            AnimatedContent(pending, label = "syncTitle") { n ->
                Text(
                    if (n > 0) stringResource(R.string.sync_card_pending, n) else stringResource(R.string.sync_card_ok),
                    style = MaterialTheme.typography.titleSmall,
                    fontWeight = FontWeight.Bold,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            Text(subtitle, style = MaterialTheme.typography.bodySmall, color = StatusColors.neutral, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        OutlinedButton(
            onClick = onRefresh,
            enabled = !sync.syncing,
            shape = CircleShape,
            contentPadding = PaddingValues(horizontal = 14.dp, vertical = 6.dp),
            border = BorderStroke(1.dp, c.outlineVariant),
            colors = ButtonDefaults.outlinedButtonColors(containerColor = c.surfaceContainerLowest, contentColor = c.primary),
        ) {
            Icon(painterResource(R.drawable.ic_sync), null, Modifier.size(16.dp).then(if (sync.syncing) Modifier.spin() else Modifier))
            Spacer(Modifier.width(6.dp))
            Text(stringResource(R.string.refresh))
        }
    }
}

@Composable
private fun OpenListButton(count: Int, onClick: () -> Unit) {
    val c = MaterialTheme.colorScheme
    BouncyButton(onClick = onClick, modifier = Modifier.fillMaxWidth().padding(top = 4.dp).heightIn(min = 56.dp)) {
        Icon(painterResource(R.drawable.ic_list), null, Modifier.size(20.dp))
        Spacer(Modifier.width(10.dp))
        Text(stringResource(R.string.lots_open_list), style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
        Spacer(Modifier.width(10.dp))
        Text(
            "$count",
            style = MaterialTheme.typography.labelLarge,
            fontWeight = FontWeight.ExtraBold,
            color = c.primary,
            modifier = Modifier.background(c.onPrimary, CircleShape).padding(horizontal = 9.dp, vertical = 2.dp),
        )
    }
}

/** FEFO'da ilk sıradaki kritik lot ("En acil: Kuzu But · SKT'ye 3 gün"); kritik lot yoksa satır yok. */
@Composable
private fun MostUrgentHint(lots: List<LotView>, today: LocalDate) {
    val lot = lots.filter { it.isCritical(today) }.minWithOrNull(fefoOrder) ?: return
    val expiry = expiryOf(lot.lot.expiryDate, today)
    val whenText = when {
        expiry.level == ExpiryLevel.EXPIRED -> stringResource(R.string.expiry_expired)
        expiry.days == 0L -> stringResource(R.string.expiry_today)
        else -> stringResource(R.string.expiry_in_days, expiry.days!!)
    }
    Text(
        stringResource(R.string.lots_most_urgent, lot.itemName, whenText),
        style = MaterialTheme.typography.bodySmall,
        color = StatusColors.neutral,
        textAlign = TextAlign.Center,
        modifier = Modifier.fillMaxWidth(),
    )
}

// ---------------------------------------------------------------------------
// Arama, durum çipleri
// ---------------------------------------------------------------------------

@Composable
private fun SearchField(query: String, onChange: (String) -> Unit) {
    OutlinedTextField(
        value = query,
        onValueChange = onChange,
        placeholder = { Text(stringResource(R.string.lots_search)) },
        leadingIcon = { Icon(painterResource(R.drawable.ic_search), null) },
        singleLine = true,
        shape = CircleShape,
        colors = OutlinedTextFieldDefaults.colors(
            unfocusedContainerColor = MaterialTheme.colorScheme.surfaceContainerLowest,
            focusedContainerColor = MaterialTheme.colorScheme.surfaceContainerLowest,
            unfocusedBorderColor = MaterialTheme.colorScheme.outlineVariant,
            unfocusedLeadingIconColor = StatusColors.neutral,
        ),
        modifier = Modifier.fillMaxWidth(),
    )
}

@Composable
private fun FilterChips(lots: List<LotView>, today: LocalDate, selected: LotFilter, onSelect: (LotFilter) -> Unit) {
    Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        LotFilter.entries.forEach { f ->
            val count = lots.count { f.matches(it, today) }
            FilterChip(stringResource(R.string.chip_label, stringResource(f.label), count), f == selected) { onSelect(f) }
        }
    }
}

@Composable
private fun FilterChip(label: String, selected: Boolean, onClick: () -> Unit) {
    val c = MaterialTheme.colorScheme
    val bg by animateColorAsState(if (selected) c.primary else c.surfaceContainerLowest, label = "chipBg")
    val fg by animateColorAsState(if (selected) c.onPrimary else c.onSurface, label = "chipFg")
    Surface(
        onClick = onClick,
        shape = CircleShape,
        color = bg,
        border = BorderStroke(1.dp, if (selected) c.primary else c.outlineVariant),
    ) {
        Text(label, style = MaterialTheme.typography.labelLarge, color = fg, modifier = Modifier.padding(horizontal = 14.dp, vertical = 9.dp))
    }
}

// ---------------------------------------------------------------------------
// Lot satırı (dokununca ayrıntılar açılır)
// ---------------------------------------------------------------------------

/** Sunucunun lot durumu -> Türkçe rozet; blocked_lots'ta olup durumu henüz güncellenmemiş lot da "Bloke". */
private fun statusLabel(status: String) = when (status) {
    "recalled" -> R.string.status_recalled
    "consumed" -> R.string.status_consumed
    else -> R.string.status_blocked
}

@Composable
private fun LotRow(
    lot: LotView,
    today: LocalDate,
    warehouse: String?,
    expanded: Boolean,
    onToggle: () -> Unit,
    onSell: () -> Unit,
    modifier: Modifier,
) {
    val c = MaterialTheme.colorScheme
    val expiry = remember(lot.lot.expiryDate, today) { expiryOf(lot.lot.expiryDate, today) }
    val critical = lot.sellable && expiry.level <= ExpiryLevel.CRITICAL
    val chevron by animateFloatAsState(if (expanded) 180f else 0f, bouncy(), label = "chevron")
    Surface(
        shape = RowShape,
        color = c.surfaceContainerLowest,
        border = BorderStroke(1.dp, if (lot.sellable) StatusColors.cardBorder else StatusColors.dangerBorder),
        modifier = modifier.sharedCard("lot-${lot.lot.id}", RowShape),
    ) {
        Column {
            Row(
                Modifier
                    .fillMaxWidth()
                    .clickable(
                        onClickLabel = stringResource(if (expanded) R.string.lot_details_hide else R.string.lot_details_show),
                        role = Role.Button,
                        onClick = onToggle,
                    )
                    .heightIn(min = 64.dp)
                    .padding(horizontal = 14.dp, vertical = 12.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                LotIcon(lot)
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    Text(lot.itemName, style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.Bold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    Text(lot.lot.lotNo, style = MaterialTheme.typography.labelMedium, color = c.primary, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
                Column(horizontalAlignment = Alignment.End, verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    Text("${lot.available.display()} ${lot.baseUom}", style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.Bold)
                    StatusPill(lot, expiry)
                }
                Icon(
                    painterResource(R.drawable.ic_expand_more),
                    null,
                    Modifier.size(20.dp).graphicsLayer { rotationZ = chevron },
                    tint = StatusColors.neutral,
                )
            }
            AnimatedVisibility(expanded, enter = expandVertically() + fadeIn(), exit = shrinkVertically() + fadeOut()) {
                LotDetails(lot, expiry, critical, warehouse, onSell)
            }
        }
    }
}

/** Baş harfler (marka mavisi) ya da bloke lotta kilit. Görsel süs: ekran okuyucudan gizli. */
@Composable
private fun LotIcon(lot: LotView) {
    val c = MaterialTheme.colorScheme
    val shape = RoundedCornerShape(12.dp)
    if (lot.sellable) {
        Box(Modifier.size(40.dp).background(c.primaryContainer, shape).clearAndSetSemantics {}, contentAlignment = Alignment.Center) {
            Text(initials(lot.itemName), style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.ExtraBold, color = c.primary)
        }
    } else {
        Box(Modifier.size(40.dp).background(c.errorContainer, shape), contentAlignment = Alignment.Center) {
            Icon(painterResource(R.drawable.ic_lock), null, Modifier.size(20.dp), tint = c.error)
        }
    }
}

/** Kritik: kalan gün (turuncu) / SKT geçti (kırmızı); bloke-geri çağrıldı (kırmızı); diğerleri "Aktif" (yeşil). */
@Composable
private fun StatusPill(lot: LotView, expiry: Expiry) {
    val c = MaterialTheme.colorScheme
    val (text, bg, fg) = when {
        !lot.sellable -> Triple(stringResource(statusLabel(lot.lot.status)), c.errorContainer, c.onErrorContainer)
        expiry.level == ExpiryLevel.EXPIRED -> Triple(stringResource(R.string.expiry_expired), c.errorContainer, c.onErrorContainer)
        expiry.level == ExpiryLevel.CRITICAL -> Triple(
            if (expiry.days == 0L) stringResource(R.string.badge_today) else stringResource(R.string.badge_days, expiry.days!!),
            c.tertiaryContainer,
            c.onTertiaryContainer,
        )
        else -> Triple(stringResource(R.string.status_active), StatusColors.successContainer, StatusColors.success)
    }
    Text(
        text,
        style = MaterialTheme.typography.labelSmall,
        fontWeight = FontWeight.Bold,
        color = fg,
        modifier = Modifier.background(bg, RoundedCornerShape(10.dp)).padding(horizontal = 8.dp, vertical = 2.dp),
    )
}

@Composable
private fun LotDetails(lot: LotView, expiry: Expiry, critical: Boolean, warehouse: String?, onSell: () -> Unit) {
    val c = MaterialTheme.colorScheme
    Column(Modifier.padding(start = 14.dp, end = 14.dp, bottom = 14.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        val expiryColor = when (expiry.level) {
            ExpiryLevel.EXPIRED -> c.error
            ExpiryLevel.CRITICAL -> c.onTertiaryContainer
            ExpiryLevel.SOON -> StatusColors.warning
            else -> c.onSurface
        }
        Row(Modifier.fillMaxWidth().background(c.background, RoundedCornerShape(12.dp)).padding(vertical = 10.dp, horizontal = 8.dp)) {
            InfoCell(stringResource(R.string.lot_remaining), "${lot.available.display()} ${lot.baseUom}", Modifier.weight(1f))
            InfoCell(stringResource(R.string.lot_price), lot.salePrice?.let { "${it.money()} €" } ?: "—", Modifier.weight(1f))
            InfoCell(stringResource(R.string.lot_expiry), expiry.date?.let(expiryDateFormat::format) ?: "—", Modifier.weight(1f), expiryColor)
        }

        if (critical) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                Icon(painterResource(R.drawable.ic_warning), null, Modifier.size(16.dp), tint = c.tertiary)
                Text(stringResource(R.string.lot_fefo_note), style = MaterialTheme.typography.bodySmall, color = c.onTertiaryContainer, fontWeight = FontWeight.SemiBold)
            }
        }
        if (!lot.sellable) {
            Column(
                Modifier.fillMaxWidth().background(c.errorContainer, RoundedCornerShape(12.dp)).padding(12.dp),
                verticalArrangement = Arrangement.spacedBy(4.dp),
            ) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    Icon(painterResource(R.drawable.ic_warning), null, Modifier.size(16.dp), tint = c.error)
                    Text(
                        stringResource(if (lot.lot.status == "recalled") R.string.lot_recalled_title else R.string.lot_blocked_title),
                        style = MaterialTheme.typography.labelLarge,
                        color = c.onErrorContainer,
                    )
                }
                Text(lot.blockedReason ?: stringResource(R.string.lot_blocked_default), style = MaterialTheme.typography.bodySmall, color = c.onErrorContainer)
            }
        }

        Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            DetailField(stringResource(R.string.lot_warehouse), warehouse ?: "—", Modifier.weight(1f))
            DetailField(stringResource(R.string.lot_unit_cost), "${lot.lot.unitCost.money()} €/${lot.baseUom}", Modifier.weight(1f))
        }

        if (lot.sellable) {
            BouncyButton(onClick = onSell, modifier = Modifier.fillMaxWidth()) {
                Icon(painterResource(R.drawable.ic_shopping_cart), null, Modifier.size(18.dp))
                Spacer(Modifier.width(8.dp))
                Text(stringResource(if (critical) R.string.lot_sell_priority else R.string.lot_sell))
            }
        }
    }
}

@Composable
private fun InfoCell(label: String, value: String, modifier: Modifier, valueColor: Color = MaterialTheme.colorScheme.onSurface) {
    Column(modifier, horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Text(label, style = MaterialTheme.typography.labelSmall, color = StatusColors.neutral)
        Text(value, style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.Bold, color = valueColor, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

@Composable
private fun DetailField(label: String, value: String, modifier: Modifier) {
    Column(modifier, verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Text(label, style = MaterialTheme.typography.labelSmall, color = StatusColors.neutral)
        Text(value, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

/** Para: 2 hane (maliyet 4 haneyle saklanır). */
private fun BigDecimal.money(): String = setScale(2, RoundingMode.HALF_UP).toPlainString()
