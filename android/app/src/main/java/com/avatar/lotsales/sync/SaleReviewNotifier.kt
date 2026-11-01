package com.avatar.lotsales.sync

import android.Manifest
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import androidx.core.app.NotificationChannelCompat
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.content.ContextCompat
import com.avatar.lotsales.MainActivity
import com.avatar.lotsales.R
import com.avatar.lotsales.data.SaleReviewUpdate
import com.avatar.lotsales.data.db.OrderStatus
import dagger.hilt.android.qualifiers.ApplicationContext
import javax.inject.Inject
import javax.inject.Singleton

/** Incelemesi sonuclanan satis icin sistem bildirimi. Izin verilmediyse atlanir; durum satis listesinde yine gorunur. */
@Singleton
class SaleReviewNotifier @Inject constructor(@ApplicationContext private val context: Context) {
    fun notify(updates: List<SaleReviewUpdate>) {
        if (updates.isEmpty()) return
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) {
            return
        }
        val manager = NotificationManagerCompat.from(context)
        manager.createNotificationChannel(
            NotificationChannelCompat.Builder(CHANNEL, NotificationManagerCompat.IMPORTANCE_DEFAULT)
                .setName(context.getString(R.string.notif_channel_reviews))
                .build(),
        )
        val open = PendingIntent.getActivity(
            context, 0,
            Intent(context, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP),
            PendingIntent.FLAG_IMMUTABLE,
        )
        for (u in updates) {
            val returned = u.status == OrderStatus.RETURNED
            val notification = NotificationCompat.Builder(context, CHANNEL)
                .setSmallIcon(R.drawable.ic_assignment_return)
                .setContentTitle(context.getString(if (returned) R.string.notif_sale_returned_title else R.string.notif_sale_reviewed_title, u.orderNo))
                .setContentText(context.getString(if (returned) R.string.notif_sale_returned_text else R.string.notif_sale_reviewed_text))
                .setContentIntent(open)
                .setAutoCancel(true)
                .build()
            manager.notify(u.orderId.hashCode(), notification) // ayni satisin yeni durumu eskisinin yerine gecer
        }
    }

    private companion object {
        const val CHANNEL = "sale-reviews"
    }
}
