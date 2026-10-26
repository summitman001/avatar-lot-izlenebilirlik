package com.avatar.lotsales.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavDestination.Companion.hasRoute
import androidx.navigation.NavGraph.Companion.findStartDestination
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
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
object SyncRoute

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AppRoot() {
    val nav = rememberNavController()
    val syncVm: SyncViewModel = hiltViewModel()
    val sync by syncVm.ui.collectAsStateWithLifecycle()
    val destination = nav.currentBackStackEntryAsState().value?.destination
    val tabs = listOf<Triple<String, Any, Boolean>>(
        Triple("Lotlar", LotsRoute, destination?.hasRoute(LotsRoute::class) == true),
        Triple("Yeni satış", NewSaleRoute(), destination?.hasRoute(NewSaleRoute::class) == true),
        Triple("Senkron", SyncRoute, destination?.hasRoute(SyncRoute::class) == true),
    )
    Scaffold(
        topBar = { TopAppBar(title = { Text("Lot Satış") }, actions = { SyncIndicator(sync) }) },
        bottomBar = {
            NavigationBar {
                tabs.forEach { (label, route, selected) ->
                    NavigationBarItem(
                        selected = selected,
                        onClick = {
                            nav.navigate(route) {
                                popUpTo(nav.graph.findStartDestination().id) { saveState = true }
                                launchSingleTop = true
                                restoreState = true
                            }
                        },
                        icon = {},
                        label = { Text(label) },
                    )
                }
            }
        },
    ) { padding ->
        NavHost(nav, startDestination = LotsRoute, modifier = Modifier.padding(padding)) {
            composable<LotsRoute> { LotsScreen(onSell = { lotId -> nav.navigate(NewSaleRoute(lotId)) }) }
            composable<NewSaleRoute> { NewSaleScreen() }
            composable<SyncRoute> { SyncScreen(syncVm) }
        }
    }
}

/** Ust bar: cevrimdisi / bekleyen N / guncel; inceleme bekleyen (flagged/rejected) varsa kirmizi uyari. */
@Composable
fun SyncIndicator(ui: SyncUi) {
    val pending = ui.status.pending
    val (text, color) = when {
        !ui.online -> (if (pending > 0) "Çevrimdışı · bekleyen $pending" else "Çevrimdışı") to Color(0xFF757575)
        pending > 0 -> "Bekleyen $pending" to Color(0xFFF57C00)
        else -> "Güncel" to Color(0xFF2E7D32)
    }
    Row {
        Chip(text, color)
        if (ui.status.problems.isNotEmpty()) Chip("İnceleme ${ui.status.problems.size}", MaterialTheme.colorScheme.error)
    }
}

@Composable
fun Chip(text: String, color: Color) {
    Text(
        text,
        color = Color.White,
        style = MaterialTheme.typography.labelMedium,
        modifier = Modifier
            .padding(end = 8.dp)
            .background(color, RoundedCornerShape(12.dp))
            .padding(horizontal = 8.dp, vertical = 4.dp),
    )
}

/** Basit secici: buton + acilir liste (Material3 ExposedDropdown API'sine bagimli olmamak icin). */
@Composable
fun <T> Picker(label: String, selected: String?, options: List<T>, optionText: (T) -> String, onPick: (T) -> Unit) {
    var open by remember { mutableStateOf(false) }
    Box(Modifier.fillMaxWidth()) {
        OutlinedButton(onClick = { open = true }, modifier = Modifier.fillMaxWidth()) {
            Text(selected ?: "$label seçin")
        }
        DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            if (options.isEmpty()) DropdownMenuItem(text = { Text("Kayıt yok (senkron bekleniyor)") }, onClick = { open = false })
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
