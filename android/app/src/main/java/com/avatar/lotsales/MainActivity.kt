package com.avatar.lotsales

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.material3.MaterialTheme
import com.avatar.lotsales.sync.SyncScheduler
import com.avatar.lotsales.ui.AppRoot
import dagger.hilt.android.AndroidEntryPoint
import javax.inject.Inject

@AndroidEntryPoint
class MainActivity : ComponentActivity() {
    @Inject lateinit var syncScheduler: SyncScheduler

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        if (savedInstanceState == null) syncScheduler.syncNow() // uygulama acilisinda senkron
        enableEdgeToEdge()
        setContent { MaterialTheme { AppRoot() } }
    }
}
