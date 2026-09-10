package io.github.rclsilver.home_notifications.service

import android.app.NotificationManager
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.util.Log
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import io.github.rclsilver.home_notifications.data.Settings
import io.github.rclsilver.home_notifications.net.ackAlert

private const val TAG = "HomeGenie"

/**
 * Acknowledges an alert from its notification.
 *
 * The central gesture of the project: stopping the reminders without
 * unlocking the phone at three in the morning. Acknowledging is not
 * resolving — the alert stays open on the server and visible in the
 * consoles; only its reminders stop.
 */
class AckReceiver : BroadcastReceiver() {

    override fun onReceive(context: Context, intent: Intent) {
        val alertId = intent.getLongExtra(EXTRA_ALERT_ID, 0)
        if (alertId == 0L) return

        val tag = intent.getStringExtra(EXTRA_TAG)
        val notificationId = intent.getIntExtra(EXTRA_NOTIFICATION_ID, 0)

        // The notification goes at once: waiting for the server's answer
        // would leave the user facing a notification that does not react,
        // and they would press again.
        if (notificationId != 0) {
            val manager =
                context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
            manager.cancel(tag, notificationId)
        }

        val pending = goAsync()
        CoroutineScope(Dispatchers.IO).launch {
            try {
                val settings = Settings(context.applicationContext)
                val url = settings.serverUrlOnce()
                val token = settings.tokenOnce()
                if (url.isEmpty() || token.isEmpty()) return@launch

                ackAlert(url, token, alertId)
                    .onSuccess { Log.i(TAG, "alert $alertId acknowledged") }
                    .onFailure {
                        // A lost acknowledgement is far worse than a lost read:
                        // the reminders would carry on. It is reported, and the
                        // alert stays acknowledgeable from the application's
                        // console.
                        Log.w(TAG, "acknowledging $alertId failed", it)
                    }
            } finally {
                pending.finish()
            }
        }
    }

    companion object {
        const val EXTRA_ALERT_ID = "alert_id"
        const val EXTRA_TAG = "tag"
        const val EXTRA_NOTIFICATION_ID = "notification_id"
    }
}
