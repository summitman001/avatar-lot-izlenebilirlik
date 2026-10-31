@file:OptIn(ExperimentalSharedTransitionApi::class)

package com.avatar.lotsales.ui

import androidx.annotation.DrawableRes
import androidx.annotation.StringRes
import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.Crossfade
import androidx.compose.animation.ExperimentalSharedTransitionApi
import androidx.compose.animation.SharedTransitionLayout
import androidx.compose.animation.SizeTransform
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.animation.expandHorizontally
import androidx.compose.animation.expandVertically
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.scaleIn
import androidx.compose.animation.shrinkHorizontally
import androidx.compose.animation.shrinkVertically
import androidx.compose.animation.slideInVertically
import androidx.compose.animation.slideOutVertically
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.NavigationBarItemDefaults
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavDestination
import androidx.navigation.NavDestination.Companion.hasRoute
import androidx.navigation.NavGraph.Companion.findStartDestination
import androidx.navigation.NavGraphBuilder
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import androidx.navigation.toRoute
import com.avatar.lotsales.R
import java.math.BigDecimal
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import kotlinx.serialization.Serializable

@Serializable
object LotsRoute

@Serializable
data class NewSaleRoute(val lotId: String? = null)

@Serializable
object SalesRoute

@Serializable
data class SaleDetailRoute(val orderId: String)

@Serializable
object SyncRoute

private data class Tab(
    @StringRes val label: Int,
    @StringRes val title: Int,
    @DrawableRes val icon: Int,
    @DrawableRes val selectedIcon: Int,
    val route: Any,
    val isCurrent: (NavDestination) -> Boolean,
)

private val tabs = listOf(
    Tab(R.string.tab_lots, R.string.title_lots, R.drawable.ic_inventory_2, R.drawable.ic_inventory_2_fill, LotsRoute) { it.hasRoute<LotsRoute>() },
    Tab(R.string.tab_new_sale, R.string.title_new_sale, R.drawable.ic_shopping_cart, R.drawable.ic_shopping_cart_fill, NewSaleRoute()) {
        it.hasRoute<NewSaleRoute>()
    },
    Tab(R.string.tab_sales, R.string.title_sales, R.drawable.ic_receipt_long, R.drawable.ic_receipt_long_fill, SalesRoute) {
        it.hasRoute<SalesRoute>() || it.hasRoute<SaleDetailRoute>()
    },
    Tab(R.string.tab_sync, R.string.title_sync, R.drawable.ic_cloud_sync, R.drawable.ic_cloud_sync_fill, SyncRoute) { it.hasRoute<SyncRoute>() },
)

// Fade-through: eski ekran hizla solar, yenisi hafif buyuyerek gelir (~310 ms).
private val enterSpec = tween<Float>(220, delayMillis = 90)

/** Ekranlara NavHost'un animasyon kapsamini verir (sharedCard icin). */
private inline fun <reified T : Any> NavGraphBuilder.screen(noinline content: @Composable () -> Unit) {
    composable<T> { CompositionLocalProvider(LocalNavScope provides this) { content() } }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AppRoot() {
    val nav = rememberNavController()
    val syncVm: SyncViewModel = hiltViewModel()
    val sync by syncVm.ui.collectAsStateWithLifecycle()
    val entry = nav.currentBackStackEntryAsState().value
    val destination = entry?.destination
    // Geri yalniz ic ekranlarda: satis detayi ve lot kartindan acilan satis (sekme kokleri Material'e gore geri tasimaz)
    val canGoBack = destination?.hasRoute<SaleDetailRoute>() == true ||
        (destination?.hasRoute<NewSaleRoute>() == true && entry.toRoute<NewSaleRoute>().lotId != null)
    val tab = tabs.firstOrNull { destination?.let(it.isCurrent) == true } ?: tabs.first()
    val title = if (destination?.hasRoute<SaleDetailRoute>() == true) R.string.title_sale_detail else tab.title
    Scaffold(
        topBar = {
            TopAppBar(
                navigationIcon = {
                    AnimatedVisibility(canGoBack, enter = fadeIn() + expandHorizontally(), exit = fadeOut() + shrinkHorizontally()) {
                        IconButton(onClick = { nav.navigateUp() }) {
                            Icon(painterResource(R.drawable.ic_arrow_back), stringResource(R.string.back))
                        }
                    }
                },
                title = { TopTitle(tab, title, showIcon = !canGoBack, sync) },
                actions = { SyncIndicator(sync) },
                colors = TopAppBarDefaults.topAppBarColors(containerColor = MaterialTheme.colorScheme.surfaceContainerLowest),
            )
        },
        bottomBar = {
            NavigationBar(containerColor = MaterialTheme.colorScheme.surfaceContainerLowest) {
                tabs.forEach { tab ->
                    val selected = destination?.let(tab.isCurrent) == true
                    NavigationBarItem(
                        selected = selected,
                        onClick = {
                            nav.navigate(tab.route) {
                                popUpTo(nav.graph.findStartDestination().id) { saveState = true }
                                launchSingleTop = true
                                restoreState = true
                            }
                        },
                        icon = { TabIcon(tab, selected) },
                        label = { Text(stringResource(tab.label), fontWeight = if (selected) FontWeight.Bold else FontWeight.Medium) },
                        colors = NavigationBarItemDefaults.colors(
                            selectedIconColor = MaterialTheme.colorScheme.primary,
                            selectedTextColor = MaterialTheme.colorScheme.primary,
                            indicatorColor = MaterialTheme.colorScheme.primaryContainer,
                        ),
                    )
                }
            }
        },
        containerColor = MaterialTheme.colorScheme.background,
    ) { padding ->
        SharedTransitionLayout(Modifier.padding(padding)) {
            CompositionLocalProvider(LocalSharedScope provides this) {
                NavHost(
                    nav,
                    startDestination = LotsRoute,
                    enterTransition = { fadeIn(enterSpec) + scaleIn(enterSpec, initialScale = 0.94f) },
                    exitTransition = { fadeOut(tween(90)) },
                    popEnterTransition = { fadeIn(enterSpec) + scaleIn(enterSpec, initialScale = 0.94f) },
                    popExitTransition = { fadeOut(tween(90)) },
                ) {
                    screen<LotsRoute> {
                        LotsScreen(onSell = { lotId -> nav.navigate(NewSaleRoute(lotId)) }, sync = sync, onRefresh = syncVm::syncNow)
                    }
                    screen<NewSaleRoute> { NewSaleScreen() }
                    screen<SalesRoute> { SalesScreen(onOpen = { id -> nav.navigate(SaleDetailRoute(id)) }) }
                    screen<SaleDetailRoute> { SaleDetailScreen() }
                    screen<SyncRoute> { SyncScreen(syncVm) }
                }
            }
        }
    }
}

/** Secilince ici dolu ikona gecer ve yaylanarak ziplar. */
@Composable
private fun TabIcon(tab: Tab, selected: Boolean) {
    val scale = remember { Animatable(1f) }
    LaunchedEffect(selected) {
        if (selected) {
            scale.animateTo(1.2f, tween(90))
            scale.animateTo(1f, bouncy())
        }
    }
    Crossfade(selected, Modifier.graphicsLayer { scaleX = scale.value; scaleY = scale.value }, label = "tabIcon") { s ->
        Icon(painterResource(if (s) tab.selectedIcon else tab.icon), null)
    }
}

/**
 * Ust bar basligi (Stitch): ikon kutusu + ekran adi, altinda renkli nokta ile senkron durumu ve cihaz deposu
 * (senkronlaniyor / cevrimdisi / bekleyen N / guncel).
 */
@Composable
private fun TopTitle(tab: Tab, @StringRes title: Int, showIcon: Boolean, ui: SyncUi) {
    val pending = ui.status.pending
    val (status, color) = when {
        ui.syncing -> stringResource(R.string.indicator_syncing) to StatusColors.warning
        !ui.online && pending > 0 -> stringResource(R.string.indicator_offline_pending, pending) to StatusColors.neutral
        !ui.online -> stringResource(R.string.indicator_offline) to StatusColors.neutral
        pending > 0 -> stringResource(R.string.indicator_pending, pending) to StatusColors.warning
        else -> stringResource(R.string.indicator_ok) to StatusColors.success
    }
    val dot by animateColorAsState(color, tween(400), label = "statusDot")
    val warehouse = ui.warehouses.firstOrNull { it.id == ui.status.warehouseId }?.code
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        AnimatedVisibility(showIcon, enter = fadeIn() + expandHorizontally(), exit = fadeOut() + shrinkHorizontally()) {
            Box(
                Modifier.size(40.dp).background(MaterialTheme.colorScheme.primaryContainer, RoundedCornerShape(12.dp)),
                contentAlignment = Alignment.Center,
            ) {
                Crossfade(tab.selectedIcon, label = "titleIcon") {
                    Icon(painterResource(it), null, Modifier.size(22.dp), tint = MaterialTheme.colorScheme.onPrimaryContainer)
                }
            }
        }
        Column {
            AnimatedContent(title, label = "title") {
                Text(stringResource(it), style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.ExtraBold)
            }
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                Box(Modifier.size(7.dp).background(dot, CircleShape))
                Text(
                    listOfNotNull(status, warehouse).joinToString(" · "),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

/** Ust bar sagi: yalniz dikkat isteyen durumlar (depo secilmedi, inceleme bekleyen kayit). */
@Composable
fun SyncIndicator(ui: SyncUi) {
    Row(Modifier.padding(end = 8.dp), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
        AnimatedVisibility(ui.status.warehouseId == null, enter = fadeIn() + expandHorizontally(), exit = fadeOut() + shrinkHorizontally()) {
            Chip(stringResource(R.string.indicator_no_warehouse), StatusColors.danger, R.drawable.ic_warning)
        }
        AnimatedVisibility(ui.status.problems.isNotEmpty(), enter = fadeIn() + expandHorizontally(), exit = fadeOut() + shrinkHorizontally()) {
            Chip(stringResource(R.string.indicator_problems, ui.status.problems.size), StatusColors.danger, R.drawable.ic_warning)
        }
    }
}

/**
 * Durum hapi: tonlu zemin + renkli metin. Renk degisimi yumusak gecer, metin degisince eskisi yukari kayar
 * ve hap ziplar (or. "Bekleyen 1" -> "Guncel").
 */
@Composable
fun Chip(text: String, color: Color, @DrawableRes icon: Int? = null, spinning: Boolean = false) {
    val c by animateColorAsState(color, tween(400), label = "chipColor")
    val bump = remember { Animatable(1f) }
    var shown by remember { mutableStateOf(text) }
    LaunchedEffect(text) {
        if (text != shown) {
            shown = text
            bump.snapTo(1.15f)
            bump.animateTo(1f, bouncy())
        }
    }
    Row(
        Modifier
            .graphicsLayer { scaleX = bump.value; scaleY = bump.value }
            .background(c.copy(alpha = 0.12f), CircleShape)
            .padding(horizontal = 10.dp, vertical = 5.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        if (icon != null) {
            Crossfade(icon, label = "chipIcon") { res ->
                Icon(painterResource(res), null, Modifier.size(14.dp).then(if (spinning) Modifier.spin() else Modifier), tint = c)
            }
        }
        AnimatedContent(
            text,
            transitionSpec = { (slideInVertically { it } + fadeIn()) togetherWith (slideOutVertically { -it } + fadeOut()) using SizeTransform(clip = false) },
            label = "chipText",
        ) { Text(it, color = c, style = MaterialTheme.typography.labelMedium) }
    }
}

/** Beyaz, yuvarlak koseli bolum karti (form ve ayar gruplari). */
@Composable
fun SectionCard(modifier: Modifier = Modifier, content: @Composable ColumnScope.() -> Unit) {
    Surface(modifier.fillMaxWidth(), shape = MaterialTheme.shapes.large, color = MaterialTheme.colorScheme.surfaceContainerLowest, shadowElevation = 1.dp) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp), content = content)
    }
}

@Composable
fun SectionLabel(text: String) =
    Text(text, style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurfaceVariant)

/** Hata/bilgi bandi: mesaj gelince acilir, gidince kapanir; hata yeni geldiginde sallanir. */
@Composable
fun MessageBanner(message: String?, isError: Boolean) {
    AnimatedContent(
        message,
        transitionSpec = { (fadeIn() + expandVertically()) togetherWith (fadeOut() + shrinkVertically()) using SizeTransform(clip = false) },
        label = "banner",
    ) { msg ->
        if (msg == null) return@AnimatedContent
        val color = if (isError) StatusColors.danger else StatusColors.success
        Row(
            Modifier
                .fillMaxWidth()
                .shake(if (isError) msg else null)
                .background(color.copy(alpha = 0.12f), MaterialTheme.shapes.medium)
                .padding(12.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Icon(painterResource(if (isError) R.drawable.ic_warning else R.drawable.ic_cloud_done), null, Modifier.size(20.dp), tint = color)
            Text(msg, color = color, style = MaterialTheme.typography.bodyMedium)
        }
    }
}

/** Basit secici: tonlu buton + acilir liste (Material3 ExposedDropdown API'sine bagimli olmamak icin). */
@Composable
fun <T> Picker(
    label: String,
    selected: String?,
    options: List<T>,
    optionText: (T) -> String,
    onPick: (T) -> Unit,
    modifier: Modifier = Modifier,
) {
    var open by remember { mutableStateOf(false) }
    val chevron by animateFloatAsState(if (open) 180f else 0f, bouncy(), label = "chevron")
    Box(modifier.fillMaxWidth()) {
        Surface(
            onClick = { open = true },
            shape = MaterialTheme.shapes.medium,
            color = MaterialTheme.colorScheme.surfaceContainerHigh,
            modifier = Modifier.fillMaxWidth(),
        ) {
            Row(Modifier.padding(horizontal = 16.dp, vertical = 14.dp), verticalAlignment = Alignment.CenterVertically) {
                AnimatedContent(selected, Modifier.weight(1f), label = "picked") { s ->
                    Text(
                        s ?: stringResource(R.string.picker_choose, label),
                        style = MaterialTheme.typography.bodyLarge,
                        color = if (s == null) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.onSurface,
                    )
                }
                Icon(painterResource(R.drawable.ic_expand_more), null, Modifier.graphicsLayer { rotationZ = chevron })
            }
        }
        DropdownMenu(expanded = open, onDismissRequest = { open = false }, shape = MaterialTheme.shapes.medium) {
            if (options.isEmpty()) DropdownMenuItem(text = { Text(stringResource(R.string.picker_empty)) }, onClick = { open = false })
            options.forEach { option ->
                DropdownMenuItem(text = { Text(optionText(option)) }, onClick = {
                    open = false
                    onPick(option)
                })
            }
        }
    }
}

fun BigDecimal.display(): String = stripTrailingZeros().toPlainString()

private val timeFormat = DateTimeFormatter.ofPattern("dd.MM.yyyy HH:mm:ss").withZone(ZoneId.systemDefault())

fun formatInstant(iso: String?): String =
    iso?.let { runCatching { timeFormat.format(Instant.parse(it)) }.getOrDefault(it) } ?: "—"
