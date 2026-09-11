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
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import io.github.rclsilver.home_notifications.net.AlertPayload
import io.github.rclsilver.home_notifications.net.fetchAlertsFiltered
import io.github.rclsilver.home_notifications.service.ConnectionService

/**
 * The state of the house on one screen.
 *
 * The question asked when opening the application is "is anything waiting
 * for me", and the answer fits in three numbers. The details are one tap
 * away, behind each of them.
 */
@Composable
fun OverviewScreen(
    serverUrl: String,
    token: String,
    unread: Int,
    onOpen: (Destination) -> Unit,
) {
    var open by remember { mutableStateOf<List<AlertPayload>>(emptyList()) }
    var error by remember { mutableStateOf("") }

    val state by ConnectionService.observedState.collectAsState()
    LaunchedEffect(serverUrl, token, state.events) {
        fetchAlertsFiltered(serverUrl, token, openOnly = true)
            .onSuccess { open = it; error = "" }
            .onFailure { error = it.message ?: "loading failed" }
    }

    val unacked = open.count { !it.isAcked }
    val critical = open.count { it.severity == "critical" }

    if (error.isNotEmpty()) {
        Text(error, color = MaterialTheme.colorScheme.error)
    }

    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        // Red is kept for what waits on somebody: an alert that is open but
        // taken is work in progress, not an emergency.
        Tile(
            value = unacked.toString(),
            label = "to handle",
            alarming = unacked > 0,
            modifier = Modifier.weight(1f),
        ) { onOpen(Destination.ALERTS) }
        Tile(
            value = open.size.toString(),
            label = "open",
            alarming = false,
            modifier = Modifier.weight(1f),
        ) { onOpen(Destination.ALERTS) }
        Tile(
            value = unread.toString(),
            label = "unread",
            alarming = false,
            modifier = Modifier.weight(1f),
        ) { onOpen(Destination.NOTIFICATIONS) }
    }

    if (critical > 0) {
        Text(
            "Including $critical critical.",
            style = MaterialTheme.typography.bodyMedium,
        )
    }

    // The connection in one line: it is what decides whether the absence of
    // alerts means "nothing to report" or "nothing is getting through".
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(
                when {
                    !state.running -> "Service stopped"
                    state.connected -> "Connected"
                    else -> "Reconnecting"
                },
                style = MaterialTheme.typography.titleSmall,
            )
            Text(
                if (state.connected) "The alerts arrive live."
                else "While the socket is down, nothing arrives — and the silence " +
                    "looks like calm.",
                style = MaterialTheme.typography.bodySmall,
            )
        }
    }

    // What needs a system setting shows here, where one looks first, and
    // nowhere at all when everything is in order.
    ReliabilityBanner(state)

    if (open.isEmpty()) return

    Text("The most recent", style = MaterialTheme.typography.titleMedium)
    open.sortedWith(compareBy({ it.isAcked }, { !it.isOpen })).take(3).forEach { alert ->
        Card(
            modifier = Modifier.fillMaxWidth(),
            onClick = { onOpen(Destination.ALERTS) },
        ) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    StatusBadge(alert)
                    SeverityBadge(alert.severity)
                    OccurrencesBadge(alert.occurrences)
                }
                Text(alert.title, style = MaterialTheme.typography.titleSmall)
                Text(
                    "${relativeAge(alert.startedAt)} · ${alert.channelSlug}",
                    style = MaterialTheme.typography.bodySmall,
                )
            }
        }
    }
}

@Composable
private fun Tile(
    value: String,
    label: String,
    alarming: Boolean,
    modifier: Modifier = Modifier,
    onClick: () -> Unit,
) {
    Card(
        modifier = modifier,
        colors = CardDefaults.cardColors(
            containerColor = if (alarming) MaterialTheme.colorScheme.errorContainer
            else MaterialTheme.colorScheme.surfaceVariant,
        ),
        onClick = onClick,
    ) {
        Column(
            Modifier.fillMaxWidth().padding(vertical = 20.dp),
            horizontalAlignment = androidx.compose.ui.Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(2.dp),
        ) {
            Text(value, style = MaterialTheme.typography.headlineMedium)
            Text(
                label,
                style = MaterialTheme.typography.bodySmall,
                textAlign = TextAlign.Center,
            )
        }
    }
}
