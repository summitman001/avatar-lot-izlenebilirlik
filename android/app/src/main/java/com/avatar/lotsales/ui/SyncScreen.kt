package com.avatar.lotsales.ui

import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.Crossfade
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.background
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
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
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
    /** bir senkron turu su an calisiyor */
    val syncing: Boolean = false,
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

    val ui = combine(connectivity.online, repo.observeStatus(), lots.observeWarehouses(), error, scheduler.running, ::SyncUi)
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
    LazyColumn(
        Modifier.fillMaxSize(),
        contentPadding = PaddingValues(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        item(key = "connection") { ConnectionCard(ui.online) }
        item(key = "warehouse") {
            SectionCard {
                SectionLabel(stringResource(R.string.device_warehouse))
                Picker(
                    stringResource(R.string.device_warehouse),
                    ui.warehouses.find { it.id == s.warehouseId }?.let { "${it.code} · ${it.name}" },
                    ui.warehouses,
                    { "${it.code} · ${it.name} (${it.warehouseType})" },
                    vm::selectWarehouse,
                )
                MessageBanner(ui.error, isError = true)
            }
        }
        item(key = "stats") {
            SectionCard {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(stringResource(R.string.pending_ops), Modifier.weight(1f), style = MaterialTheme.typography.titleMedium)
                    RollingText(
                        s.pending.toString(),
                        style = MaterialTheme.typography.headlineSmall.copy(fontWeight = FontWeight.ExtraBold),
                        color = if (s.pending > 0) StatusColors.warning else StatusColors.success,
                    )
                }
                Text(stringResource(R.string.last_sync, formatInstant(s.lastSyncAt)), style = MaterialTheme.typography.bodyMedium)
                Text(
                    stringResource(R.string.last_error, s.lastError ?: "—"),
                    style = MaterialTheme.typography.bodyMedium,
                    color = if (s.lastError != null) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
        item(key = "sync-button") {
            BouncyButton(onClick = vm::syncNow, modifier = Modifier.fillMaxWidth()) {
                Icon(painterResource(R.drawable.ic_sync), null, Modifier.size(20.dp).then(if (ui.syncing) Modifier.spin() else Modifier))
                AnimatedContent(ui.syncing, transitionSpec = { fadeIn() togetherWith fadeOut() }, label = "syncLabel") { running ->
                    Text(stringResource(if (running) R.string.sync_running else R.string.sync_now), Modifier.padding(start = 8.dp))
                }
            }
        }
        item(key = "problems-title") {
            Text(stringResource(R.string.problems_title), style = MaterialTheme.typography.titleMedium, modifier = Modifier.padding(top = 8.dp))
        }
        if (s.problems.isEmpty()) {
            item(key = "none") {
                Text(stringResource(R.string.none), color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.animateItem())
            }
        }
        items(s.problems, key = { it.opId }) { op ->
            ProblemCard(op, onAck = { vm.acknowledge(op) }, onDiscard = { vm.discard(op) }, Modifier.animateItem())
        }
    }
}

/** Cevrimici/cevrimdisi: renk ve ikon yumusak gecer. */
@Composable
private fun ConnectionCard(online: Boolean) {
    val color by animateColorAsState(if (online) StatusColors.success else StatusColors.neutral, tween(500), label = "conn")
    Row(
        Modifier
            .fillMaxWidth()
            .background(color.copy(alpha = 0.12f), MaterialTheme.shapes.large)
            .padding(16.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        Box(Modifier.size(44.dp).background(color, CircleShape), contentAlignment = Alignment.Center) {
            Crossfade(online, label = "connIcon") {
                Icon(painterResource(if (it) R.drawable.ic_cloud_done else R.drawable.ic_cloud_off), null, tint = Color.White)
            }
        }
        AnimatedContent(online, transitionSpec = { fadeIn() togetherWith fadeOut() }, label = "connText") {
            Text(stringResource(if (it) R.string.conn_online else R.string.conn_offline), style = MaterialTheme.typography.titleMedium, color = color)
        }
    }
}

@Composable
private fun ProblemCard(op: OutboxEntity, onAck: () -> Unit, onDiscard: () -> Unit, modifier: Modifier) {
    val flagged = op.status == OutboxStatus.FLAGGED
    val color = if (flagged) StatusColors.warning else StatusColors.danger
    Surface(modifier.fillMaxWidth(), shape = MaterialTheme.shapes.large, color = color.copy(alpha = 0.08f)) {
        Column(Modifier.padding(start = 16.dp, top = 14.dp, end = 8.dp, bottom = 4.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(op.entity, Modifier.weight(1f), style = MaterialTheme.typography.titleSmall)
                Chip(stringResource(if (flagged) R.string.status_flagged else R.string.status_rejected), color, R.drawable.ic_warning)
            }
            Text(formatInstant(op.createdAt), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            // flagged: ihlal kodlari -> metin; rejected: sunucunun serbest metni
            Text(if (flagged) violationText(op.lastError) else op.lastError.orEmpty(), style = MaterialTheme.typography.bodyMedium)
            if (flagged) {
                TextButton(onClick = onAck, modifier = Modifier.align(Alignment.End)) { Text(stringResource(R.string.ack)) }
            } else {
                TextButton(onClick = onDiscard, modifier = Modifier.align(Alignment.End)) { Text(stringResource(R.string.discard)) }
            }
        }
    }
}
