package com.avatar.lotsales

import android.Manifest
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import android.view.animation.AccelerateInterpolator
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.core.content.ContextCompat
import androidx.core.splashscreen.SplashScreen.Companion.installSplashScreen
import com.avatar.lotsales.sync.SyncScheduler
import com.avatar.lotsales.ui.AppRoot
import com.avatar.lotsales.ui.LotSalesTheme
import dagger.hilt.android.AndroidEntryPoint
import javax.inject.Inject

@AndroidEntryPoint
class MainActivity : ComponentActivity() {
    @Inject lateinit var syncScheduler: SyncScheduler

    // Reddedilirse bildirim gelmez; satis durumu listede yine guncellenir.
    private val notificationPermission = registerForActivityResult(ActivityResultContracts.RequestPermission()) {}

    override fun onCreate(savedInstanceState: Bundle?) {
        installSplashScreen().setOnExitAnimationListener { splash ->
            // API 31+'da sistem ikonu gostermediyse (or. adb ile acilis) iconView yoktur
            runCatching { splash.iconView }.getOrNull()?.animate()?.scaleX(1.4f)?.scaleY(1.4f)?.setDuration(300L)?.start()
            splash.view.animate().alpha(0f).setDuration(300L).setInterpolator(AccelerateInterpolator())
                .withEndAction { splash.remove() }.start()
        }
        super.onCreate(savedInstanceState)
        if (savedInstanceState == null) syncScheduler.syncNow() // uygulama acilisinda senkron
        if (savedInstanceState == null && Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) {
            notificationPermission.launch(Manifest.permission.POST_NOTIFICATIONS)
        }
        enableEdgeToEdge()
        setContent { LotSalesTheme { AppRoot() } }
    }
}
