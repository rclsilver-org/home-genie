package io.github.rclsilver.home_notifications.net

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/** Payload of a `message.new` event. */
@Serializable
data class MessagePayload(
    val id: Long,
    @SerialName("channel_id") val channelId: Long,
    @SerialName("channel_slug") val channelSlug: String = "",
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
