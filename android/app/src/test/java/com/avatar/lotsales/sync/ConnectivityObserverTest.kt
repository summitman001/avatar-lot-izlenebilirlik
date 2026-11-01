package com.avatar.lotsales.sync

import android.content.Context
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.toList
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Shadows.shadowOf
import org.robolectric.shadows.ShadowNetworkCapabilities

/**
 * Varsayilan ag kaybolunca gosterge cevrimdisina gecer. Android onLost'u cagirdiginda activeNetwork kaybolan
 * agi bir sure daha (internet yetenegiyle) dondurebilir; gosterge buna bakip "Guncel"de takili kalmamali.
 */
@RunWith(AndroidJUnit4::class)
class ConnectivityObserverTest {
    private val context = ApplicationProvider.getApplicationContext<Context>()
    private val cm = context.getSystemService(ConnectivityManager::class.java)

    @Test
    fun lostDefaultNetworkGoesOfflineEvenWhileActiveNetworkStillReportsInternet() = runTest {
        val network = cm.activeNetwork!!
        val internet = ShadowNetworkCapabilities.newInstance()
        shadowOf(internet).addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
        shadowOf(cm).setNetworkCapabilities(network, internet)

        val values = mutableListOf<Boolean>()
        backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) { ConnectivityObserver(context).online.toList(values) }
        runCurrent()
        val callback = shadowOf(cm).networkCallbacks.single()

        callback.onLost(network) // activeNetwork henuz guncellenmedi: hala internetli ag
        runCurrent()
        callback.onAvailable(network)
        runCurrent()

        assertEquals(listOf(true, false, true), values)
    }
}
