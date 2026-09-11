package io.github.rclsilver.home_notifications.net

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/** Payload of a `message.new` event. */
@Serializable
data class MessagePayload(
    val id: Long,
    @SerialName("channel_id") val channelId: Long,
    @SerialName("channel_slug") val channelSlug: String = "",
    // Present when the message reports an alert: it is what decides whether
    // there is an "Acknowledge" button in the notification.
    @SerialName("alert_id") val alertId: Long? = null,
    val title: String = "",
    val body: String = "",
    val priority: Int = 3,
    val tags: List<String> = emptyList(),
    @SerialName("click_url") val clickUrl: String = "",
    val read: Boolean = false,
)

/** Payload of a `messages.read` event, sent by my other devices. */
@Serializable
data class MessagesReadPayload(
    @SerialName("channel_id") val channelId: Long,
    @SerialName("message_ids") val messageIds: List<Long> = emptyList(),
)

@Serializable
data class ChannelPayload(
    val id: Long,
    val slug: String,
    val name: String = "",
    val description: String = "",
    val role: String = "",
    val unread: Int = 0,
)

/** An Alertmanager alert, as the server exposes it. */
@Serializable
data class AlertPayload(
    val id: Long,
    @SerialName("channel_id") val channelId: Long,
    // The home console mixes several channels: an alert without its origin
    // is unusable there.
    @SerialName("channel_slug") val channelSlug: String = "",
    val status: String = "",
    val severity: String = "",
    val title: String = "",
    val body: String = "",
    val labels: Map<String, String> = emptyMap(),
    @SerialName("started_at") val startedAt: String = "",
    @SerialName("acked_by") val ackedBy: String = "",
    @SerialName("acked_at") val ackedAt: String? = null,
    // What tells an alert that beats from a stable one.
    val occurrences: Int = 1,
    @SerialName("reminder_count") val reminderCount: Int = 0,
    val annotations: Map<String, String> = emptyMap(),
    @SerialName("generator_url") val generatorUrl: String = "",
) {
    val isAcked: Boolean get() = ackedAt != null
    val isOpen: Boolean get() = status == "firing"
}

/**
 * A reminder cadence.
 *
 * [scope] is "default" for a per-severity default, "channel" for an override
 * belonging to the channel. The most specific wins, so showing the
 * scope avoids the belief that one is editing a value that does not apply.
 */
@Serializable
data class ReminderPolicyPayload(
    val severity: String,
    @SerialName("interval_seconds") val intervalSeconds: Int = 0,
    @SerialName("quiet_from") val quietFrom: String = "",
    @SerialName("quiet_to") val quietTo: String = "",
    val enabled: Boolean = false,
    val scope: String = "",
) {
    val isOverride: Boolean get() = scope == "channel"
    val intervalMinutes: Int get() = intervalSeconds / 60
}

/**
 * A publish token, the one a machine carries.
 *
 * [token] is only filled in at creation: the cleartext value is shown once,
 * and the listing never shows it again.
 */
@Serializable
data class PublishTokenPayload(
    val id: Long,
    val name: String = "",
    @SerialName("last_used_at") val lastUsedAt: String? = null,
    @SerialName("revoked_at") val revokedAt: String? = null,
    val token: String = "",
) {
    val isRevoked: Boolean get() = revokedAt != null
}

@Serializable
data class CreateChannelRequest(
    val slug: String,
    val name: String = "",
    val description: String = "",
)

@Serializable
data class CreateTokenRequest(val name: String)

/** One step in a message's life, for a user and a device. */
@Serializable
data class TimelineEntryPayload(
    val kind: String,
    val username: String = "",
    val device: String = "",
    val at: String = "",
)

/** An alert together with its journal. */
@Serializable
data class AlertDetailPayload(
    val alert: AlertPayload,
    val log: List<AlertLogEntryPayload> = emptyList(),
)

@Serializable
data class AlertLogEntryPayload(
    val at: String = "",
    val kind: String = "",
    val detail: String = "",
)

/**
 * A member of a channel.
 *
 * [role] is "owner", "writer" or "reader" — the server's vocabulary, kept as
 * it is so the screen and the API speak of the same thing.
 */
@Serializable
data class MemberPayload(
    @SerialName("user_id") val userId: Long,
    val username: String = "",
    @SerialName("display_name") val displayName: String = "",
    val role: String = "",
)

/** Response of `/api/v1/messages/unread`. */
@Serializable
data class UnreadCountPayload(val count: Int = 0)
