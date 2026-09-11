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
 *
 * Not to be confused with quiet hours: a mute **drops**, quiet hours merely
 * silence. What arrives while muted produces no notification, and nothing
 * catches it up.
 */
@Composable
private fun MuteRow(channel: ChannelPayload, serverUrl: String, token: String) {
    val scope = rememberCoroutineScope()
    // The state comes from the server when the screen opens, then from what
    // was just done: the channel list is not reloaded from this screen.
    var mutedUntil by remember(channel.id) {
        mutableStateOf(if (channel.isMuted) channel.mutedUntil else null)
    }
    var error by remember(channel.id) { mutableStateOf("") }

    Text("Mute", style = MaterialTheme.typography.titleMedium)
    Text(
        mutedUntil?.let { "Muted until ${localTime(it)} — nothing will arrive before then." }
            ?: "No mute. This channel's messages notify normally.",
        style = MaterialTheme.typography.bodySmall,
    )

    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        listOf(1L, 4L, 24L).forEach { hours ->
            TextButton(onClick = {
                scope.launch {
                    val until = Instant.now().plus(hours, ChronoUnit.HOURS)
                        .truncatedTo(ChronoUnit.SECONDS)
                    muteChannel(serverUrl, token, channel.id, until.toString())
                        .onSuccess { mutedUntil = until.toString(); error = "" }
                        .onFailure { error = it.message ?: "failed" }
                }
            }) { Text("${hours}h") }
        }
        // Nothing to lift when nothing is set: the button only shows up when
        // it has something to undo.
        if (mutedUntil != null) {
            TextButton(onClick = {
                scope.launch {
                    // An empty string lifts the mute, as the API wants.
                    muteChannel(serverUrl, token, channel.id, "")
                        .onSuccess { mutedUntil = null; error = "" }
                        .onFailure { error = it.message ?: "failed" }
                }
            }) { Text("Unmute") }
        }
    }
    if (error.isNotEmpty()) {
        Text(error, color = MaterialTheme.colorScheme.error,
            style = MaterialTheme.typography.bodySmall)
    }
}

/** A readable local time, from an RFC3339 instant. */
private fun localTime(rfc3339: String): String = runCatching {
    java.time.format.DateTimeFormatter.ofPattern("HH:mm")
        .withZone(java.time.ZoneId.systemDefault())
        .format(java.time.Instant.parse(rfc3339))
}.getOrElse { rfc3339 }
