package io.github.rclsilver.home_notifications.ui

import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.FilterChip
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
import io.github.rclsilver.home_notifications.net.ackAlert
import io.github.rclsilver.home_notifications.net.fetchAlertsFiltered
import io.github.rclsilver.home_notifications.net.unackAlert
import io.github.rclsilver.home_notifications.service.ConnectionService

/**
 * The filters, in the order they are wanted.
 *
 * "Open" first, because that is the question one asks this screen: what is
 * still running. The unacknowledged have no tab of their own — they are the
 * ones that rise to the top of the open list, and one more tab for a subset
 * already singled out would cost more than it returns.
 */
private enum class AlertFilter(val label: String) {
    OPEN("Open"),
    CRITICAL("Critical"),
    CLOSED("Closed"),
    ALL("All"),
}

/**
 * The alert console — the home screen and the main function of the
 * application: knowing what is open, who has taken it, and acknowledging it.
 */
@Composable
fun AlertsScreen(serverUrl: String, token: String, onOpen: (AlertPayload) -> Unit) {
    val scope = rememberCoroutineScope()
    var filter by remember { mutableStateOf(AlertFilter.OPEN) }
    var alerts by remember { mutableStateOf<List<AlertPayload>>(emptyList()) }
    var error by remember { mutableStateOf("") }
    var reloads by remember { mutableStateOf(0) }

    val state by ConnectionService.observedState.collectAsState()
    LaunchedEffect(filter, state.events, reloads) {
        fetchAlertsFiltered(
            serverUrl, token,
            openOnly = filter == AlertFilter.OPEN || filter == AlertFilter.CRITICAL,
            closedOnly = filter == AlertFilter.CLOSED,
            severity = if (filter == AlertFilter.CRITICAL) "critical" else "",
        ).onSuccess { alerts = it; error = "" }
            .onFailure { error = it.message ?: "loading failed" }
    }

    Row(
        modifier = Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        AlertFilter.entries.forEach { candidate ->
            FilterChip(
                selected = candidate == filter,
                onClick = { filter = candidate },
                label = { Text(candidate.label) },
            )
        }
    }

    if (error.isNotEmpty()) {
        Text(error, color = MaterialTheme.colorScheme.error)
    }

    if (alerts.isEmpty()) {
        Text(
            when (filter) {
                AlertFilter.OPEN -> "Nothing open."
                AlertFilter.CRITICAL -> "No critical alert."
                AlertFilter.CLOSED -> "Nothing closed."
                AlertFilter.ALL -> "No alert."
            },
            style = MaterialTheme.typography.bodyLarge,
        )
        return
    }

    // In the live views, what is unacknowledged rises: that is what is waiting
    // for somebody. The history keeps the server's order, which sorts by
    // resolution date — sorting it otherwise would erase exactly what one
    // comes there to find.
    val ordered =
        if (filter == AlertFilter.CLOSED) alerts
        else alerts.sortedWith(compareBy({ it.isAcked }, { !it.isOpen }))
    ordered.forEach { alert ->
        AlertRow(
            alert = alert,
            onOpen = { onOpen(alert) },
            onAck = {
                scope.launch { ackAlert(serverUrl, token, alert.id).onSuccess { reloads++ } }
            },
            onUnack = {
                scope.launch { unackAlert(serverUrl, token, alert.id).onSuccess { reloads++ } }
            },
        )
    }
}

@Composable
private fun AlertRow(
    alert: AlertPayload,
    onOpen: () -> Unit,
    onAck: () -> Unit,
    onUnack: () -> Unit,
) {
    // The background only shouts for what demands an action: an alert taken
    // or resolved goes back to neutral, or the screen is red permanently and
    // signals nothing at all.
    val container = when {
        !alert.isOpen -> MaterialTheme.colorScheme.surfaceVariant
        alert.isAcked -> MaterialTheme.colorScheme.surface
        else -> MaterialTheme.colorScheme.errorContainer
    }

    Card(
        modifier = Modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(containerColor = container),
        onClick = onOpen,
    ) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Row(
                horizontalArrangement = Arrangement.spacedBy(6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                StatusBadge(alert)
                SeverityBadge(alert.severity)
                OccurrencesBadge(alert.occurrences)
                Text("#${alert.id}", style = MaterialTheme.typography.labelSmall)
            }

            Text(
                alert.title,
                style = MaterialTheme.typography.titleSmall,
                fontWeight = if (alert.isOpen && !alert.isAcked) FontWeight.Bold
                else FontWeight.Normal,
            )

            Text(
                buildString {
                    append(relativeAge(alert.startedAt))
                    append(" · ").append(alert.channelSlug)
                    alert.labels["instance"]?.let { append(" · ").append(it) }
                    // "nobody" rather than a blank: the absence of a taker is
                    // the information, not a missing value.
                    append(" · ").append(if (alert.isAcked) alert.ackedBy else "nobody")
                },
                style = MaterialTheme.typography.bodySmall,
            )

            if (alert.isOpen) {
                if (alert.isAcked) {
                    TextButton(onClick = onUnack) { Text("Un-acknowledge") }
                } else {
                    TextButton(onClick = onAck) { Text("Acknowledge") }
                }
            }
        }
    }
}
