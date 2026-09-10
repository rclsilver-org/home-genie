package io.github.rclsilver.home_notifications.ui

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
import io.github.rclsilver.home_notifications.net.AlertPayload
import io.github.rclsilver.home_notifications.net.ChannelPayload
import io.github.rclsilver.home_notifications.net.MessagePayload
import io.github.rclsilver.home_notifications.net.ackAlert
import io.github.rclsilver.home_notifications.net.fetchAlerts
import io.github.rclsilver.home_notifications.net.fetchMessages
import io.github.rclsilver.home_notifications.net.markChannelReadUpTo
import io.github.rclsilver.home_notifications.net.markRead
import io.github.rclsilver.home_notifications.net.muteChannel
import java.time.Instant
import java.time.temporal.ChronoUnit
import io.github.rclsilver.home_notifications.service.ConnectionService

/**
 * A channel's feed, plus the console of its open alerts.
 *
 * The open alerts come first and are not mixed into the feed: what demands an
 * action must be visible without scrolling, and an alert stays open until it
 * is resolved, whereas a message is a past event.
 */
@Composable
fun ChannelScreen(
    channel: ChannelPayload,
    serverUrl: String,
    token: String,
    onBack: () -> Unit,
) {
    val scope = rememberCoroutineScope()
    var messages by remember { mutableStateOf<List<MessagePayload>>(emptyList()) }
    var alerts by remember { mutableStateOf<List<AlertPayload>>(emptyList()) }
    var error by remember { mutableStateOf("") }

    // Reloaded on every event received on the socket: the list follows what
    // arrives without duplicating the server's logic on the client side.
    val state by ConnectionService.observedState.collectAsState()
    LaunchedEffect(channel.id, state.events) {
        fetchMessages(serverUrl, token, channel.id)
            .onSuccess { messages = it; error = "" }
            .onFailure { error = it.message ?: "loading failed" }
        fetchAlerts(serverUrl, token, channel.id, openOnly = true)
            .onSuccess { alerts = it }
    }

    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column {
            Text(channel.name.ifEmpty { channel.slug },
                style = MaterialTheme.typography.headlineSmall)
            Text(channel.slug, style = MaterialTheme.typography.bodySmall)
        }
        TextButton(onClick = onBack) { Text("Retour") }
    }

    if (error.isNotEmpty()) {
        Text(error, color = MaterialTheme.colorScheme.error)
    }

    if (alerts.isNotEmpty()) {
        Text("Alertes ouvertes", style = MaterialTheme.typography.titleMedium)
        alerts.forEach { alert ->
            AlertCard(alert) {
                scope.launch {
                    ackAlert(serverUrl, token, alert.id)
                    fetchAlerts(serverUrl, token, channel.id, openOnly = true)
                        .onSuccess { alerts = it }
                }
            }
        }
    }

    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text("Messages", style = MaterialTheme.typography.titleMedium)
        if (messages.any { !it.read }) {
            TextButton(onClick = {
                scope.launch {
                    markChannelReadUpTo(serverUrl, token, channel.id, 0)
                    fetchMessages(serverUrl, token, channel.id)
                        .onSuccess { messages = it }
                }
            }) { Text("Mark all read") }
        }
    }

    if (messages.isEmpty()) {
        Text("no message", style = MaterialTheme.typography.bodySmall)
    }

    MuteRow(channel, serverUrl, token)

    RemindersSection(channel.id, serverUrl, token)

    TokensSection(channel.id, channel.slug, serverUrl, token)

    Text("Messages", style = MaterialTheme.typography.titleMedium)
    messages.forEach { message ->
        MessageCard(message) {
            scope.launch {
                markRead(serverUrl, token, message.id)
                fetchMessages(serverUrl, token, channel.id)
                    .onSuccess { messages = it }
            }
        }
    }
}

@Composable
private fun AlertCard(alert: AlertPayload, onAck: () -> Unit) {
    Card(
        modifier = Modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(
            containerColor = MaterialTheme.colorScheme.errorContainer,
        ),
    ) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(alert.title, style = MaterialTheme.typography.titleSmall)
            Text(alert.body, style = MaterialTheme.typography.bodySmall)
            Text(
                alert.labels.entries.joinToString(" · ") { "${it.key}=${it.value}" },
                style = MaterialTheme.typography.bodySmall,
            )
            if (alert.isAcked) {
                // Acknowledged but still open: show who took it, because that
                // is the information ntfy was missing.
                Text("taken by ${alert.ackedBy}", style = MaterialTheme.typography.labelMedium)
            } else {
                TextButton(onClick = onAck) { Text("Acknowledge") }
            }
        }
    }
}

@Composable
private fun MessageCard(message: MessagePayload, onRead: () -> Unit) {
    Card(modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(
                message.title.ifEmpty { "(untitled)" },
                style = MaterialTheme.typography.titleSmall,
                // Bold carries the unread state: discreet, and specific to the
                // caller — a message read by one member stays bold for the others.
                fontWeight = if (message.read) FontWeight.Normal else FontWeight.Bold,
            )
            if (message.body.isNotEmpty()) {
                Text(message.body, style = MaterialTheme.typography.bodyMedium)
            }
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    "priority ${message.priority}" +
                        if (message.tags.isEmpty()) "" else " · " + message.tags.joinToString(", "),
                    style = MaterialTheme.typography.bodySmall,
                )
                if (!message.read) {
                    TextButton(onClick = onRead) { Text("Lu") }
                }
            }
        }
    }
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
