package org.plebian.app

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.IBinder

/**
 * The foreground service (type connectedDevice) that holds the Omi's Bluetooth
 * link and the line to the server with the app closed. It runs no code of its
 * own: it keeps the process, and with it the cached engine, alive, and starts
 * that engine if the system restarts the service without the app.
 */
class LinkService : Service() {
    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        Engine.ensure(this)
        val name = applicationInfo.loadLabel(packageManager)
        val manager = getSystemService(NotificationManager::class.java)
        manager.createNotificationChannel(NotificationChannel(CHANNEL, name, NotificationManager.IMPORTANCE_MIN))
        val open = PendingIntent.getActivity(
            this, 0, packageManager.getLaunchIntentForPackage(packageName), PendingIntent.FLAG_IMMUTABLE,
        )
        val notification = Notification.Builder(this, CHANNEL)
            .setSmallIcon(R.mipmap.ic_launcher)
            .setContentTitle(name)
            .setContentIntent(open)
            .setOngoing(true)
            .build()
        startForeground(ID, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_CONNECTED_DEVICE)
        return START_STICKY
    }

    companion object {
        const val CHANNEL = "link"
        const val ID = 1
    }
}
