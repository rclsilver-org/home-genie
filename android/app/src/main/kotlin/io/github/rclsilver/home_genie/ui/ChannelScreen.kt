package io.github.rclsilver.home_genie.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import io.github.rclsilver.home_genie.net.AlertPayload
import io.github.rclsilver.home_genie.net.ChannelPayload
import io.github.rclsilver.home_genie.net.MessagePayload
import io.github.rclsilver.home_genie.net.ackAlert
import io.github.rclsilver.home_genie.net.fetchAlerts
import io.github.rclsilver.home_genie.net.fetchMessages
import io.github.rclsilver.home_genie.net.markChannelReadUpTo
import io.github.rclsilver.home_genie.net.markRead
import io.github.rclsilver.home_genie.net.muteChannel
import java.time.Instant
import java.time.temporal.ChronoUnit
import io.github.rclsilver.home_genie.service.ConnectionService

/**
 * A channel's administration page.
 *
 * Nothing is read here: neither the alerts nor the messages. They have screens
 * of their own, where they are read across every channel at once, and
 * repeating them here made this page two things at once — somewhere one comes
 * to change something, and somewhere one comes to see what is happening. The
 * settings drowned under the feed.
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

    MuteRow(channel, serverUrl, token)

    RemindersSection(channel.id, serverUrl, token)

    QuietHoursSection(channel.id, serverUrl, token)

    MembersSection(channel.id, serverUrl, token, isOwner = channel.role == "owner")

    TokensSection(channel.id, channel.slug, serverUrl, token)
}

/**
 * The channel's mute.
 *
 * A relative duration and not a date: "one hour" is what one wants during
 * maintenance, and computing an instant by hand on a phone is a chore. The
 * mute is an attribute of the channel, so it holds for all of its members —
 * it is not a personal setting.
 */
@Composable
private fun MuteRow(channel: ChannelPayload, serverUrl: String, token: String) {
    val scope = rememberCoroutineScope()
    var state by remember(channel.id) { mutableStateOf("") }

    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text("Mute", style = MaterialTheme.typography.titleMedium)
        listOf(1L, 4L, 24L).forEach { hours ->
            TextButton(onClick = {
                scope.launch {
                    val until = Instant.now().plus(hours, ChronoUnit.HOURS)
                        .truncatedTo(ChronoUnit.SECONDS)
                    muteChannel(serverUrl, token, channel.id, until.toString())
                        .onSuccess { state = "muted for ${hours}h" }
                        .onFailure { state = it.message ?: "failed" }
                }
            }) { Text("${hours}h") }
        }
        TextButton(onClick = {
            scope.launch {
                // An empty string lifts the mute, as the API wants.
                muteChannel(serverUrl, token, channel.id, "")
                    .onSuccess { state = "mute lifted" }
                    .onFailure { state = it.message ?: "failed" }
            }
        }) { Text("Lever") }
    }
    if (state.isNotEmpty()) {
        Text(state, style = MaterialTheme.typography.bodySmall)
    }
}
