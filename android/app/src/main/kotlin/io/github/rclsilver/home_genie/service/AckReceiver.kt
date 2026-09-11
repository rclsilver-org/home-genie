package io.github.rclsilver.home_genie.service

import android.app.NotificationManager
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

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

        // The send goes through WorkManager rather than a receiver's ten
        // seconds of reprieve: that one dies with the process, and a lost
        // acknowledgement leaves the reminders running on an alert the user
        // believes handled.
        AckWorker.enqueue(context.applicationContext, alertId)
    }

    companion object {
        const val EXTRA_ALERT_ID = "alert_id"
        const val EXTRA_TAG = "tag"
        const val EXTRA_NOTIFICATION_ID = "notification_id"
    }
}
