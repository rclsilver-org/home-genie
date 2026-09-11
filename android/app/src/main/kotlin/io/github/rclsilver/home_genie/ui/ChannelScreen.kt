package io.github.rclsilver.home_genie.ui

import androidx.compose.foundation.layout.Column
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import io.github.rclsilver.home_genie.net.ChannelPayload

/**
 * A channel's administration page.
 *
 * Nothing is read here: neither the alerts nor the messages. They have screens
 * of their own, where they are read across every channel at once, and
 * repeating them here made this page two things at once — somewhere one comes
 * to change something, and somewhere one comes to see what is happening. The
 * settings drowned under the feed.
 *
 * The mute is not here either: it became personal and channel-wide, because
 * the gesture one actually makes is "be quiet, I am working on it", and
 * setting it channel by channel was a way to forget one. It lives in the
 * settings.
 */
@Composable
fun ChannelScreen(
    channel: ChannelPayload,
    serverUrl: String,
    token: String,
) {
    Column {
        Text(channel.name.ifEmpty { channel.slug },
            style = MaterialTheme.typography.headlineSmall)
        Text(channel.slug, style = MaterialTheme.typography.bodySmall)
    }

    RemindersSection(channel.id, serverUrl, token)

    QuietHoursSection(channel.id, serverUrl, token)

    MembersSection(channel.id, serverUrl, token, isOwner = channel.role == "owner")

    TokensSection(channel.id, channel.slug, serverUrl, token)
}
