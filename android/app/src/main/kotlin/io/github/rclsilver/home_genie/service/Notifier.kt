package io.github.rclsilver.home_genie.service

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.os.Bundle
import io.github.rclsilver.home_genie.MainActivity
import io.github.rclsilver.home_genie.net.MessagePayload

/** The message id carried by the notification, so it can be found again. */
private const val EXTRA_MESSAGE_ID = "io.github.rclsilver.home_genie.MESSAGE_ID"

/**
 * Shows the messages received.
 *
 * One Android notification channel per (server channel, priority) pair:
 * that is what lets sound and importance be tuned finely from the system
 * settings, and lets a maximum priority pierce Do Not Disturb without the
 * download notices doing the same.
 */
class Notifier(private val context: Context) {

    private val manager =
        context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager

    fun show(message: MessagePayload) {
        val channelId = ensureChannel(message.channelSlug, message.priority)
        // An alert has a single notification, whatever the number of
        // reminders: a reminder replaces the one it repeats. Stacking fifteen
        // of them adds nothing and drowns everything else.
        val tag = message.alertId?.let { "alert-$it" } ?: message.channelSlug
        val notificationId = (message.alertId ?: message.id).toInt()

        val open = PendingIntent.getActivity(
            context, notificationId,
            Intent(context, MainActivity::class.java),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )

        // Swiping means "read", not "acknowledged": the two gestures are
        // distinct, and an unacknowledged alert comes back at the next reminder.
        val dismissed = PendingIntent.getBroadcast(
            context, notificationId,
            Intent(context, DismissReceiver::class.java)
                .putExtra(DismissReceiver.EXTRA_MESSAGE_ID, message.id),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )

        val title = message.title.ifEmpty { message.channelSlug }
        val text = message.body.ifEmpty { message.title }

        val builder = Notification.Builder(context, channelId)
            .setContentTitle(title)
            .setContentText(text)
            .setStyle(Notification.BigTextStyle().bigText(text))
            .setSmallIcon(android.R.drawable.stat_notify_chat)
            .setContentIntent(open)
            .setDeleteIntent(dismissed)
            .setAutoCancel(true)
            .setWhen(System.currentTimeMillis())

        // The rank of the reminder goes in the header line, not in the title:
        // prefixing "Reminder 3 — " truncated the title on the lock screen,
        // which is exactly where it must be read at a glance.
        if (message.reminderCount > 0) {
            builder.setSubText("Reminder ${message.reminderCount}")
            builder.setNumber(message.reminderCount)
        }

        // The category decides the fate of the notification under Do Not
        // Disturb: an alert at maximum priority is an alarm, everything else
        // is a message.
        builder.setCategory(
            if (message.priority >= 5) Notification.CATEGORY_ALARM
            else Notification.CATEGORY_MESSAGE
        )

        // "Acknowledge" only appears on a message reporting an alert. It is
        // the central gesture: stopping the reminders without unlocking the
        // phone. Distinct from swiping, which only marks it read and lets the
        // alert come back at the next reminder.
        message.alertId?.let { alertId ->
            val ack = PendingIntent.getBroadcast(
                context, alertId.toInt(),
                Intent(context, AckReceiver::class.java)
                    .putExtra(AckReceiver.EXTRA_ALERT_ID, alertId)
                    .putExtra(AckReceiver.EXTRA_TAG, tag)
                    .putExtra(AckReceiver.EXTRA_NOTIFICATION_ID, notificationId),
                PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
            )
            builder.addAction(
                Notification.Action.Builder(null, "Acknowledge", ack).build()
            )
        }

        // "Mute 1 h": the answer to a series — a season downloading, a rule
        // beating through a known operation. Absent from the alerts, where
        // muting the channel would amount to turning off what one came to
        // watch; there, acknowledging is the right gesture.
        if (message.alertId == null) {
            val mute = PendingIntent.getBroadcast(
                context, message.channelId.toInt(),
                Intent(context, MuteReceiver::class.java)
                    .putExtra(MuteReceiver.EXTRA_CHANNEL_ID, message.channelId)
                    .putExtra(MuteReceiver.EXTRA_TAG, tag)
                    .putExtra(MuteReceiver.EXTRA_NOTIFICATION_ID, notificationId),
                PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
            )
            builder.addAction(
                Notification.Action.Builder(null, "Mute 1 h", mute).build()
            )
        }

        // The message shown is no longer the one naming the notification: a
        // read performed elsewhere carries a message id, and that is how it
        // has to be found.
        builder.addExtras(Bundle().apply { putLong(EXTRA_MESSAGE_ID, message.id) })

        manager.notify(tag, notificationId, builder.build())
    }

    /**
     * The Android channel matching (server channel, priority).
     *
     * Priority 5 pierces Do Not Disturb — that is the reason an alert channel
     * exists: to wake someone. Android honours that flag only if notification
     * policy access has been granted, and ignores it silently otherwise; the
     * reliability screen is the only place where that gap becomes visible.
     * manque devient visible.
     */
    private fun ensureChannel(slug: String, priority: Int): String {
        val id = "ch_${slug}_p$priority"
        val bypass = priority >= 5 && manager.isNotificationPolicyAccessGranted

        val existing = manager.getNotificationChannel(id)
        if (existing != null) {
            // The importance of an existing channel can no longer change, but
            // Do Not Disturb bypass can: the permission may have been granted
            // after the first alert.
            if (bypass && !existing.canBypassDnd()) {
                manager.createNotificationChannel(existing.apply { setBypassDnd(true) })
            }
            return id
        }

        val importance = when (priority) {
            5 -> NotificationManager.IMPORTANCE_HIGH
            4 -> NotificationManager.IMPORTANCE_HIGH
            3 -> NotificationManager.IMPORTANCE_DEFAULT
            2 -> NotificationManager.IMPORTANCE_LOW
            else -> NotificationManager.IMPORTANCE_MIN
        }

        manager.createNotificationChannel(
            NotificationChannel(id, "$slug — priority $priority", importance).apply {
                description = "Priority $priority messages of channel $slug"
                setBypassDnd(bypass)
            }
        )
        return id
    }

    /**
     * Withdraws the notifications of messages read elsewhere.
     *
     * Matching happens on the message id carried in the extras, not on the
     * notification id: since a reminder replaces its alert's notification,
     * the two no longer coincide.
     */
    fun cancelAll(messageIds: List<Long>) {
        if (messageIds.isEmpty()) return
        val wanted = messageIds.toSet()
        manager.activeNotifications
            .filter { it.notification.extras.getLong(EXTRA_MESSAGE_ID, 0) in wanted }
            .forEach { manager.cancel(it.tag, it.id) }
    }
}
