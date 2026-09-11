package io.github.rclsilver.home_notifications.service

import android.app.NotificationManager
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

/**
 * Mutes for an hour, from one of the notifications.
 *
 * The answer to a series: the mediacenter downloading a whole season, or a
 * rule beating through a known operation. An hour is long enough to cover
 * the episode and short enough that nobody discovers three days later that
 * they hear nothing any more — the mute lifts on its own.
 */
class MuteReceiver : BroadcastReceiver() {

    override fun onReceive(context: Context, intent: Intent) {
        val channelId = intent.getLongExtra(EXTRA_CHANNEL_ID, 0)
        if (channelId == 0L) return

        val tag = intent.getStringExtra(EXTRA_TAG)
        val notificationId = intent.getIntExtra(EXTRA_NOTIFICATION_ID, 0)
        if (notificationId != 0) {
            val manager =
                context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
            manager.cancel(tag, notificationId)
        }

        // Same reasoning as for the acknowledgement: the gesture must survive
        // having no network, or the series keeps ringing.
        MuteWorker.enqueue(context.applicationContext, channelId)
    }

    companion object {
        const val EXTRA_CHANNEL_ID = "channel_id"
        const val EXTRA_TAG = "tag"
        const val EXTRA_NOTIFICATION_ID = "notification_id"
    }
}
