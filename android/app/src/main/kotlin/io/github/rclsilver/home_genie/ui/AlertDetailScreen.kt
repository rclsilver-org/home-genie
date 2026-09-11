package io.github.rclsilver.home_genie.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Tab
import androidx.compose.material3.TabRow
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
import io.github.rclsilver.home_genie.net.AlertDetailPayload
import io.github.rclsilver.home_genie.net.AlertLogEntryPayload
import io.github.rclsilver.home_genie.net.ackAlert
import io.github.rclsilver.home_genie.net.fetchAlertDetail
import io.github.rclsilver.home_genie.net.unackAlert
import io.github.rclsilver.home_genie.service.ConnectionService

/**
 * An alert in detail: what it says, and what happened to it.
 *
 * Two tabs rather than one long page: opening an alert to act on it, one
 * wants the summary; opening it to understand why the same rule woke
 * someone three times, one wants the journal. Mixing the two forces a
 * scroll in both cases.
 */
@Composable
fun AlertDetailScreen(serverUrl: String, token: String, alertId: Long) {
    val scope = rememberCoroutineScope()
    var detail by remember { mutableStateOf<AlertDetailPayload?>(null) }
    var error by remember { mutableStateOf("") }
    var tab by remember { mutableStateOf(0) }
    var reloads by remember { mutableStateOf(0) }

    // The timeline has to follow the life of the alert: a reminder sent while
    // the screen is open must show up without leaving and coming back.
    val state by ConnectionService.observedState.collectAsState()
    LaunchedEffect(alertId, state.events, reloads) {
        fetchAlertDetail(serverUrl, token, alertId)
            .onSuccess { detail = it; error = "" }
            .onFailure { error = it.message ?: "loading failed" }
    }

    if (error.isNotEmpty()) {
        Text(error, color = MaterialTheme.colorScheme.error)
        return
    }

    val payload = detail ?: run {
        Text("Loading…")
        return
    }
    val alert = payload.alert

    Row(
        horizontalArrangement = Arrangement.spacedBy(6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        StatusBadge(alert)
        SeverityBadge(alert.severity)
        OccurrencesBadge(alert.occurrences)
        Text("#${alert.id}", style = MaterialTheme.typography.labelSmall)
    }

    Text(alert.title, style = MaterialTheme.typography.titleLarge)

    if (alert.isOpen) {
        if (alert.isAcked) {
            // Hand the alert back to nobody: it resumes its reminder cadence.
            TextButton(onClick = {
                scope.launch { unackAlert(serverUrl, token, alert.id).onSuccess { reloads++ } }
            }) { Text("Un-acknowledge") }
        } else {
            Button(onClick = {
                scope.launch { ackAlert(serverUrl, token, alert.id).onSuccess { reloads++ } }
            }) { Text("Acknowledge") }
        }
    }

    TabRow(selectedTabIndex = tab) {
        Tab(selected = tab == 0, onClick = { tab = 0 }, text = { Text("Detail") })
        Tab(
            selected = tab == 1,
            onClick = { tab = 1 },
            // The entry count in the tab saves having to open it to learn
            // whether anything happened in there.
            text = { Text("Timeline (${payload.log.size})") },
        )
    }

    if (tab == 0) {
        if (alert.body.isNotEmpty()) {
            Text(alert.body, style = MaterialTheme.typography.bodyMedium)
        }
        DefinitionRow("Channel", alert.channelSlug)
        DefinitionRow("Opened", relativeAge(alert.startedAt))
        DefinitionRow("Acknowledged by", if (alert.isAcked) alert.ackedBy else "nobody")
        if (alert.reminderCount > 0) {
            DefinitionRow("Reminders sent", alert.reminderCount.toString())
        }
        if (alert.annotations.isNotEmpty()) {
            Text("Annotations", style = MaterialTheme.typography.titleSmall)
            alert.annotations.toSortedMap().forEach { (k, v) -> DefinitionRow(k, v) }
        }
        if (alert.labels.isNotEmpty()) {
            Text("Labels", style = MaterialTheme.typography.titleSmall)
            alert.labels.toSortedMap().forEach { (k, v) -> DefinitionRow(k, v) }
        }
        if (alert.generatorUrl.isNotEmpty()) {
            DefinitionRow("Source", alert.generatorUrl)
        }
    } else {
        if (payload.log.isEmpty()) {
            Text("Nothing yet.")
        }
        payload.log.forEach { LogRow(it) }
    }
}

@Composable
private fun DefinitionRow(label: String, value: String) {
    Column(Modifier.fillMaxWidth().padding(vertical = 2.dp)) {
        Text(label, style = MaterialTheme.typography.labelSmall)
        Text(value, style = MaterialTheme.typography.bodyMedium)
    }
}

@Composable
private fun LogRow(entry: AlertLogEntryPayload) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(
                logTitle(entry.kind),
                style = MaterialTheme.typography.titleSmall,
                fontWeight = FontWeight.Bold,
            )
            if (entry.detail.isNotEmpty()) {
                Text(entry.detail, style = MaterialTheme.typography.bodySmall)
            }
            Text(relativeAge(entry.at), style = MaterialTheme.typography.labelSmall)
        }
    }
}

/** The server's vocabulary, translated once, here. */
private fun logTitle(kind: String): String = when (kind) {
    "opened" -> "Alert opened"
    "notified" -> "Notification sent"
    "reminded" -> "Reminder"
    "repeated" -> "Alert repeated"
    "acked" -> "Acknowledged"
    "resolved" -> "Resolved"
    else -> kind
}
