package com.avatar.lotsales

import android.os.Bundle
import android.view.animation.AccelerateInterpolator
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.core.splashscreen.SplashScreen.Companion.installSplashScreen
import com.avatar.lotsales.sync.SyncScheduler
import com.avatar.lotsales.ui.AppRoot
import com.avatar.lotsales.ui.LotSalesTheme
import dagger.hilt.android.AndroidEntryPoint
import javax.inject.Inject

@AndroidEntryPoint
class MainActivity : ComponentActivity() {
    @Inject lateinit var syncScheduler: SyncScheduler

    override fun onCreate(savedInstanceState: Bundle?) {
        installSplashScreen().setOnExitAnimationListener { splash ->
            // API 31+'da sistem ikonu gostermediyse (or. adb ile acilis) iconView yoktur
            runCatching { splash.iconView }.getOrNull()?.animate()?.scaleX(1.4f)?.scaleY(1.4f)?.setDuration(300L)?.start()
            splash.view.animate().alpha(0f).setDuration(300L).setInterpolator(AccelerateInterpolator())
                .withEndAction { splash.remove() }.start()
        }
        super.onCreate(savedInstanceState)
        if (savedInstanceState == null) syncScheduler.syncNow() // uygulama acilisinda senkron
        enableEdgeToEdge()
        setContent { LotSalesTheme { AppRoot() } }
    }
}
