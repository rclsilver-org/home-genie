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
    val status: String = "",
    val severity: String = "",
    val title: String = "",
    val body: String = "",
    val labels: Map<String, String> = emptyMap(),
    @SerialName("started_at") val startedAt: String = "",
    @SerialName("acked_by") val ackedBy: String = "",
    @SerialName("acked_at") val ackedAt: String? = null,
) {
    val isAcked: Boolean get() = ackedAt != null
    val isOpen: Boolean get() = status == "firing"
}
