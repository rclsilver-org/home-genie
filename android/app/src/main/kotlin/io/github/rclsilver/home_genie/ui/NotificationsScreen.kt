package io.github.rclsilver.home_genie.ui

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.Row
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import io.github.rclsilver.home_genie.net.fetchFeed
import io.github.rclsilver.home_genie.net.markFeedRead
import io.github.rclsilver.home_genie.net.markRead
import io.github.rclsilver.home_genie.net.MessagePayload
import io.github.rclsilver.home_genie.service.ConnectionService

/**
 * The notification feed: what the media tools and the image watcher publish.
 *
 * Separate from the alert console because these objects do not live the
 * same way. A notification is read or unread, per person, and nothing else
 * ever happens to it; an alert opens, is taken, reminds and closes, for
 * everybody at once. Mixing them would force each to borrow the other's
 * vocabulary.
 *
 * Reading is personal: emptying this view empties it for nobody else —
 * which is the whole point of a shared feed.
 */
@Composable
fun NotificationsScreen(serverUrl: String, token: String) {
    val scope = rememberCoroutineScope()
    // Unread by default: the view exists to be emptied.
    var unreadOnly by remember { mutableStateOf(true) }
    var messages by remember { mutableStateOf<List<MessagePayload>>(emptyList()) }
    var error by remember { mutableStateOf("") }
    var reloads by remember { mutableStateOf(0) }

    val state by ConnectionService.observedState.collectAsState()
    LaunchedEffect(unreadOnly, state.events, reloads) {
        fetchFeed(serverUrl, token, unreadOnly)
            .onSuccess { messages = it; error = "" }
            .onFailure { error = it.message ?: "loading failed" }
    }

    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        FilterChip(
            selected = unreadOnly,
            onClick = { unreadOnly = true },
            label = { Text("Unread") },
        )
        FilterChip(
            selected = !unreadOnly,
            onClick = { unreadOnly = false },
            label = { Text("All") },
        )
        if (messages.any { !it.read }) {
            TextButton(onClick = {
                scope.launch { markFeedRead(serverUrl, token).onSuccess { reloads++ } }
            }) { Text("Mark all read") }
        }
    }

    if (error.isNotEmpty()) {
        Text(error, color = MaterialTheme.colorScheme.error)
    }

    if (messages.isEmpty()) {
        Text(
            if (unreadOnly) "Nothing new." else "No notification.",
            style = MaterialTheme.typography.bodyLarge,
        )
        return
    }

    messages.forEach { message ->
        NotificationRow(message, serverUrl, token) {
            scope.launch { markRead(serverUrl, token, message.id).onSuccess { reloads++ } }
        }
    }
}

@Composable
private fun NotificationRow(message: MessagePayload, serverUrl: String, token: String,
                            onRead: () -> Unit) {
    // Collapsed by default: the timeline is a diagnostic tool, not something
    // wanted on every line of the feed. It followed the messages here when
    // the channel page became administrative — this is the only screen where
    // a message is still read.
    var showTimeline by remember(message.id) { mutableStateOf(false) }

    Card(
        modifier = Modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(
            containerColor = if (message.read) MaterialTheme.colorScheme.surface
            else MaterialTheme.colorScheme.surfaceVariant,
        ),
        border = BorderStroke(1.dp, MaterialTheme.colorScheme.outline),
        // Opening means reading: it is the only gesture these objects expect,
        // and asking for a second tap on a "mark as read" button would be
        // ceremony for nothing.
        onClick = { if (!message.read) onRead() },
    ) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(
                message.title.ifEmpty { message.channelSlug },
                style = MaterialTheme.typography.titleSmall,
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
                    buildString {
                        append(message.channelSlug)
                        if (message.tags.isNotEmpty()) {
                            append(" · ").append(message.tags.joinToString(", "))
                        }
                    },
                    style = MaterialTheme.typography.bodySmall,
                )
                TextButton(onClick = { showTimeline = !showTimeline }) {
                    Text(if (showTimeline) "Hide" else "Distribution")
                }
            }

            if (showTimeline) {
                TimelinePanel(message.id, serverUrl, token)
            }
        }
    }
}
