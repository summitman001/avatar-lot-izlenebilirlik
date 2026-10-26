package com.avatar.lotsales.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.ListItem
import androidx.compose.material3.ListItemDefaults
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import com.avatar.lotsales.data.LotRepository
import com.avatar.lotsales.data.LotView
import com.avatar.lotsales.data.fefoOrder
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import javax.inject.Inject

@HiltViewModel
class LotsViewModel @Inject constructor(repo: LotRepository) : ViewModel() {
    val query = MutableStateFlow("")
    val lots = combine(repo.observeLots(), query) { lots, q ->
        lots.filter { q.isBlank() || it.lot.lotNo.contains(q, ignoreCase = true) || it.itemName.contains(q, ignoreCase = true) }
            .sortedWith(compareBy<LotView> { it.itemName }.then(fefoOrder))
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), emptyList())
}

@Composable
fun LotsScreen(onSell: (String) -> Unit, vm: LotsViewModel = hiltViewModel()) {
    val query by vm.query.collectAsStateWithLifecycle()
    val lots by vm.lots.collectAsStateWithLifecycle()
    Column(Modifier.fillMaxSize()) {
        OutlinedTextField(
            value = query,
            onValueChange = { vm.query.value = it },
            label = { Text("Ara (lot no / ürün)") },
            singleLine = true,
            modifier = Modifier.fillMaxWidth().padding(12.dp),
        )
        if (lots.isEmpty()) Text("Lot yok. Senkron sekmesinden senkronlayın.", Modifier.padding(16.dp))
        LazyColumn {
            items(lots, key = { it.lot.id }) { lot ->
                LotRow(lot, onClick = { onSell(lot.lot.id) })
                HorizontalDivider()
            }
        }
    }
}

@Composable
private fun LotRow(lot: LotView, onClick: () -> Unit) {
    val blockedRed = Color(0xFFC62828)
    ListItem(
        headlineContent = { Text(lot.itemName) },
        supportingContent = {
            Text("${lot.lot.lotNo} · SKT: ${lot.lot.expiryDate ?: "—"} · Kullanılabilir: ${lot.available.display()} ${lot.baseUom}")
        },
        trailingContent = {
            if (lot.sellable) Chip("Aktif", Color(0xFF2E7D32)) else Chip(lot.lot.status.uppercase(), blockedRed)
        },
        colors = if (lot.sellable) ListItemDefaults.colors() else ListItemDefaults.colors(containerColor = Color(0xFFFFEBEE)),
        // bloke/geri cagrilmis lot tiklanamaz
        modifier = if (lot.sellable) Modifier.clickable(onClick = onClick) else Modifier,
    )
}
