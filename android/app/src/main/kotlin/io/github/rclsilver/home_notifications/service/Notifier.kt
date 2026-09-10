package io.github.rclsilver.home_notifications.service

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import io.github.rclsilver.home_notifications.MainActivity
import io.github.rclsilver.home_notifications.net.MessagePayload

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

        val open = PendingIntent.getActivity(
            context, message.id.toInt(),
            Intent(context, MainActivity::class.java),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )

        // Swiping means "read", not "acknowledged": the two gestures are
        // distinct, and an unacknowledged alert comes back at the next reminder.
        val dismissed = PendingIntent.getBroadcast(
            context, message.id.toInt(),
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

        // "Acknowledge" only appears on a message reporting an alert. It is
        // the central gesture: stopping the reminders without unlocking the
        // phone. Distinct from swiping, which only marks it read and lets the
        // alert come back at the next reminder.
        message.alertId?.let { alertId ->
            val ack = PendingIntent.getBroadcast(
                context, alertId.toInt(),
                Intent(context, AckReceiver::class.java)
                    .putExtra(AckReceiver.EXTRA_ALERT_ID, alertId)
                    .putExtra(AckReceiver.EXTRA_TAG, message.channelSlug)
                    .putExtra(AckReceiver.EXTRA_NOTIFICATION_ID, message.id.toInt()),
                PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
            )
            builder.addAction(
                Notification.Action.Builder(null, "Acknowledge", ack).build()
            )
        }

        val notification = builder.build()

        // The tag is the message identifier: an update of the same message
        // replaces the notification instead of stacking one more.
        manager.notify(message.channelSlug, message.id.toInt(), notification)
    }

    private fun ensureChannel(slug: String, priority: Int): String {
        val id = "ch_${slug}_p$priority"
        if (manager.getNotificationChannel(id) != null) return id

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
            }
        )
        return id
    }

    /**
     * Withdraws the notifications of messages read elsewhere.
     *
     * The remote event does not carry the slug, and the tag is built from it;
     * so the displayed notifications are swept to find the matching ones,
     * rather than guessing a tag.
     */
    fun cancelAll(messageIds: List<Long>) {
        if (messageIds.isEmpty()) return
        val wanted = messageIds.map { it.toInt() }.toSet()
        manager.activeNotifications
            .filter { it.id in wanted }
            .forEach { manager.cancel(it.tag, it.id) }
    }
}
