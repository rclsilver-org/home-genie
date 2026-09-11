package io.github.rclsilver.home_genie.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import io.github.rclsilver.home_genie.net.TimelineEntryPayload
import io.github.rclsilver.home_genie.net.fetchTimeline
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter

/** What each step means, in plain words. */
private val LABELS = mapOf(
    "queued" to "queued",
    "sent" to "sent",
    "delivered" to "received and confirmed",
    "read" to "read",
    "acked" to "acknowledged",
    "dismissed" to "swiped away",
)

private val TIME = DateTimeFormatter.ofPattern("dd/MM HH:mm:ss").withZone(ZoneId.systemDefault())

/**
 * A message's distribution timeline: to whom, on which device, and when at
 * each step.
 *
 * Its point is not the list itself but what it makes visible: a `sent` that
 * nothing confirms is the signature of a socket the system froze without
 * closing — the case where the server believes it delivered and nothing
 * arrived. The panel states that explicitly rather than leaving a missing
 * line to be noticed on its own.
 */
@Composable
fun TimelinePanel(messageId: Long, serverUrl: String, token: String) {
    var entries by remember(messageId) { mutableStateOf<List<TimelineEntryPayload>>(emptyList()) }
    var error by remember(messageId) { mutableStateOf("") }

    LaunchedEffect(messageId) {
        fetchTimeline(serverUrl, token, messageId)
            .onSuccess { entries = it; error = "" }
            .onFailure { error = it.message ?: "timeline unavailable" }
    }

    if (error.isNotEmpty()) {
        Text(error, style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.error)
        return
    }
    if (entries.isEmpty()) {
        Text("no trace", style = MaterialTheme.typography.bodySmall)
        return
    }

    Column(
        modifier = Modifier.fillMaxWidth().padding(top = 8.dp),
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        entries.forEach { entry ->
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
            ) {
                Text(
                    buildString {
                        append(LABELS[entry.kind] ?: entry.kind)
                        append(" · ").append(entry.username)
                        if (entry.device.isNotEmpty()) append(" · ").append(entry.device)
                    },
                    style = MaterialTheme.typography.bodySmall,
                )
                Text(format(entry.at), style = MaterialTheme.typography.bodySmall)
            }
        }

        unconfirmed(entries).forEach { device ->
            Text(
                "sent to '$device' but never confirmed",
                style = MaterialTheme.typography.bodySmall,
                fontWeight = FontWeight.Bold,
                color = MaterialTheme.colorScheme.error,
            )
        }
    }
}

/**
 * The devices the message was sent to without them confirming it.
 *
 * The comparison is per device and not global: the phone may have confirmed
 * while the tablet heard nothing, and that is the distinction that matters.
 */
private fun unconfirmed(entries: List<TimelineEntryPayload>): List<String> {
    val sent = entries.filter { it.kind == "sent" && it.device.isNotEmpty() }
        .map { it.device }.toSet()
    val delivered = entries.filter { it.kind == "delivered" }.map { it.device }.toSet()
    return (sent - delivered).sorted()
}

private fun format(rfc3339: String): String =
    runCatching { TIME.format(Instant.parse(rfc3339)) }.getOrElse { rfc3339 }
