package com.avatar.lotsales.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Button
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import com.avatar.lotsales.R
import com.avatar.lotsales.data.LotRepository
import com.avatar.lotsales.data.SalesRepository
import com.avatar.lotsales.data.SyncRepository
import com.avatar.lotsales.data.SyncStatus
import com.avatar.lotsales.data.WarehouseChangeBlocked
import com.avatar.lotsales.data.db.OutboxEntity
import com.avatar.lotsales.data.db.OutboxStatus
import com.avatar.lotsales.data.db.WarehouseEntity
import com.avatar.lotsales.domain.violationText
import com.avatar.lotsales.sync.ConnectivityObserver
import com.avatar.lotsales.sync.SyncScheduler
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import javax.inject.Inject

data class SyncUi(
    val online: Boolean = true,
    val status: SyncStatus = SyncStatus(),
    val warehouses: List<WarehouseEntity> = emptyList(),
    val error: String? = null,
)

@HiltViewModel
class SyncViewModel @Inject constructor(
    private val repo: SyncRepository,
    private val sales: SalesRepository,
    lots: LotRepository,
    connectivity: ConnectivityObserver,
    private val scheduler: SyncScheduler,
) : ViewModel() {
    private val error = MutableStateFlow<String?>(null)

    val ui = combine(connectivity.online, repo.observeStatus(), lots.observeWarehouses(), error, ::SyncUi)
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), SyncUi())

    fun syncNow() = scheduler.syncNow()

    fun selectWarehouse(w: WarehouseEntity) {
        viewModelScope.launch {
            try {
                repo.setDeviceWarehouse(w.id)
                error.value = null
                scheduler.syncNow() // yeni deponun bakiyeleriyle tam yeniden cekim
            } catch (e: WarehouseChangeBlocked) {
                error.value = e.message
            }
        }
    }

    fun acknowledge(op: OutboxEntity) {
        viewModelScope.launch { sales.acknowledge(op.opId) }
    }

    fun discard(op: OutboxEntity) {
        viewModelScope.launch { sales.discardRejected(op.opId) }
    }
}

@Composable
fun SyncScreen(vm: SyncViewModel) {
    val ui by vm.ui.collectAsStateWithLifecycle()
    val s = ui.status
    Column(Modifier.fillMaxSize().padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(stringResource(if (ui.online) R.string.conn_online else R.string.conn_offline))
        Text(stringResource(R.string.device_warehouse), style = MaterialTheme.typography.labelLarge)
        Picker(
            stringResource(R.string.device_warehouse),
            ui.warehouses.find { it.id == s.warehouseId }?.let { "${it.code} · ${it.name}" },
            ui.warehouses,
            { "${it.code} · ${it.name} (${it.warehouseType})" },
            vm::selectWarehouse,
        )
        ui.error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
        Text(stringResource(R.string.pending_ops, s.pending))
        Text(stringResource(R.string.last_sync, formatInstant(s.lastSyncAt)))
        Text(
            stringResource(R.string.last_error, s.lastError ?: "—"),
            color = if (s.lastError != null) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurface,
        )
        Button(onClick = vm::syncNow, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.sync_now)) }
        Text(stringResource(R.string.problems_title), style = MaterialTheme.typography.titleMedium)
        if (s.problems.isEmpty()) Text(stringResource(R.string.none))
        LazyColumn {
            items(s.problems, key = { it.opId }) { op ->
                val flagged = op.status == OutboxStatus.FLAGGED
                ListItem(
                    headlineContent = { Text("${op.entity} · ${stringResource(if (flagged) R.string.status_flagged else R.string.status_rejected)}") },
                    // flagged: ihlal kodlari -> metin; rejected: sunucunun serbest metni
                    supportingContent = { Text("${formatInstant(op.createdAt)}\n${if (flagged) violationText(op.lastError) else op.lastError.orEmpty()}") },
                    trailingContent = {
                        if (flagged) {
                            TextButton(onClick = { vm.acknowledge(op) }) { Text(stringResource(R.string.ack)) }
                        } else {
                            TextButton(onClick = { vm.discard(op) }) { Text(stringResource(R.string.discard)) }
                        }
                    },
                )
                HorizontalDivider()
            }
        }
    }
}
