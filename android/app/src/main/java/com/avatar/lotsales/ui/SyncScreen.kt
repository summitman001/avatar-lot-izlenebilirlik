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
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import com.avatar.lotsales.data.SyncRepository
import com.avatar.lotsales.data.SyncStatus
import com.avatar.lotsales.sync.ConnectivityObserver
import com.avatar.lotsales.sync.SyncScheduler
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import javax.inject.Inject

data class SyncUi(val online: Boolean = true, val status: SyncStatus = SyncStatus())

@HiltViewModel
class SyncViewModel @Inject constructor(
    repo: SyncRepository,
    connectivity: ConnectivityObserver,
    private val scheduler: SyncScheduler,
) : ViewModel() {
    val ui = combine(connectivity.online, repo.observeStatus(), ::SyncUi)
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), SyncUi())

    fun syncNow() = scheduler.syncNow()
}

@Composable
fun SyncScreen(vm: SyncViewModel) {
    val ui by vm.ui.collectAsStateWithLifecycle()
    val s = ui.status
    Column(Modifier.fillMaxSize().padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(if (ui.online) "Bağlantı: çevrimiçi" else "Bağlantı: çevrimdışı")
        Text("Bekleyen işlem: ${s.pending}")
        Text("Son başarılı senkron: ${formatInstant(s.lastSyncAt)}")
        Text("Son hata: ${s.lastError ?: "—"}", color = if (s.lastError != null) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurface)
        Button(onClick = vm::syncNow, modifier = Modifier.fillMaxWidth()) { Text("Şimdi senkronla") }
        Text("İnceleme gerekenler (flagged / rejected)", style = MaterialTheme.typography.titleMedium)
        if (s.problems.isEmpty()) Text("Yok")
        LazyColumn {
            items(s.problems, key = { it.opId }) { op ->
                ListItem(
                    headlineContent = { Text("${op.entity} · ${op.status.uppercase()}") },
                    supportingContent = { Text("${formatInstant(op.createdAt)}\n${op.lastError.orEmpty()}") },
                )
                HorizontalDivider()
            }
        }
    }
}
