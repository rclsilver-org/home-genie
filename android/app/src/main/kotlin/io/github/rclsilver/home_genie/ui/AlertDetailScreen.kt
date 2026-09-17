package io.github.rclsilver.home_genie.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Tab
import androidx.compose.material3.TabRow
import androidx.compose.material3.Text
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
import androidx.compose.ui.text.font.FontFamily
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

    // The title first and large: on this screen one already knows which alert
    // was opened, so the badges no longer have to introduce it.
    // Severity and state together, above the title: both qualify what
    // follows rather than sharing a line with it.
    Row(
        horizontalArrangement = Arrangement.spacedBy(6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        SeverityBadge(alert.severity)
        StatusBadge(alert)
        OccurrencesBadge(alert.occurrences)
    }

    Text(alert.title, style = MaterialTheme.typography.titleLarge)

    // Channel, age and holder on one line. Who has it is the question the
    // status badge raises and does not answer, so it is answered right here
    // rather than three sections further down.
    Text(
        buildString {
            append(alert.channelSlug)
            append(" · ").append(relativeAge(alert.startedAt))
            append(" · ").append(if (alert.isAcked) alert.ackedBy else "nobody")
        },
        style = MaterialTheme.typography.bodySmall,
    )

    LabelChips(alert.labels, max = 6)

    // What broke, then what to do about it — both before the tabs. Everything
    // above this point identifies the alert; everything below it is reference
    // one consults, and reference should not sit between a reader and the
    // button they opened the screen to press.
    if (alert.body.isNotEmpty()) {
        Text("Description", style = MaterialTheme.typography.titleSmall)
        // In a fixed-width face: it carries paths, exit codes and thresholds
        // copied out of a monitoring rule, and proportional text makes those
        // harder to read than they need be.
        Card(
            Modifier.fillMaxWidth(),
            colors = CardDefaults.cardColors(
                containerColor = MaterialTheme.colorScheme.surfaceVariant,
            ),
        ) {
            Text(
                alert.body,
                style = MaterialTheme.typography.bodySmall,
                fontFamily = FontFamily.Monospace,
                modifier = Modifier.padding(12.dp),
            )
        }
    }

    if (alert.isOpen) {
        if (alert.isAcked) {
            // Hand the alert back to nobody: it resumes its reminder cadence.
            OutlinedButton(
                modifier = Modifier.fillMaxWidth(),
                onClick = {
                    scope.launch { unackAlert(serverUrl, token, alert.id).onSuccess { reloads++ } }
                },
            ) { Text("Un-acknowledge") }
        } else {
            Button(
                modifier = Modifier.fillMaxWidth(),
                onClick = {
                    scope.launch { ackAlert(serverUrl, token, alert.id).onSuccess { reloads++ } }
                },
            ) { Text("Acknowledge") }
        }
    }

    TabRow(selectedTabIndex = tab) {
        // Named for what it holds. "Detail" was true when the tab carried
        // the description and the facts; both have moved above, and a tab
        // promising detail and delivering a label table is a small lie.
        Tab(selected = tab == 0, onClick = { tab = 0 }, text = { Text("Labels") })
        Tab(
            selected = tab == 1,
            onClick = { tab = 1 },
            // The entry count in the tab saves having to open it to learn
            // whether anything happened in there.
            text = { Text("Timeline (${payload.log.size})") },
        )
    }

    if (tab == 0) {
        // No table of facts any more. Status, channel, severity, when it
        // opened and who took it are all in the header now, and a card
        // repeating them underneath was a second telling of one thing.
        //
        // The labels are what is left that the header cannot carry: a
        // key/value table, because a chip can only show the value and
        // `speedtest` means nothing without knowing it answers "service".
        if (alert.labels.isNotEmpty()) {
            // No heading: the tab above already says "Labels".
            Card(Modifier.fillMaxWidth()) {
                Column(Modifier.padding(12.dp)) {
                    alert.labels.toSortedMap().forEach { (k, v) -> DefinitionRow(k, v) }
                }
            }
        }

        val extra = alert.annotations.filterKeys { it !in ANNOTATIONS_SHOWN_ELSEWHERE }
        if (extra.isNotEmpty()) {
            Text("Annotations", style = MaterialTheme.typography.titleSmall)
            Card(Modifier.fillMaxWidth()) {
                Column(Modifier.padding(12.dp)) {
                    extra.toSortedMap().forEach { (k, v) -> DefinitionRow(k, v) }
                }
            }
        }

        if (alert.generatorUrl.isNotEmpty()) {
            Card(Modifier.fillMaxWidth()) {
                Column(Modifier.padding(12.dp)) {
                    DefinitionRow("Source", alert.generatorUrl)
                }
            }
        }
    } else {
        if (payload.log.isEmpty()) {
            Text("Nothing yet.")
        }
        payload.log.forEach { LogRow(it) }
    }
}

/**
 * The annotations that already appear elsewhere on this screen.
 *
 * Alertmanager's `summary` is what the title shows and `description` is the
 * block above; listing them again under "Annotations" printed the same
 * sentence twice within one scroll.
 */
private val ANNOTATIONS_SHOWN_ELSEWHERE = setOf("summary", "description")

// Two columns rather than two stacked lines. A dozen facts read as a table,
// where the eye runs down the labels and stops at the one it wants; stacked,
// the same dozen becomes two dozen lines and the page has to be scrolled to
// be counted.
@Composable
private fun DefinitionRow(label: String, value: String) {
    Row(
        modifier = Modifier.fillMaxWidth().padding(vertical = 3.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(
            label,
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.width(120.dp),
        )
        // The value takes the rest and wraps: a generator URL is long, and
        // cutting it would hide the only part that differs between two rules.
        Text(
            value,
            style = MaterialTheme.typography.bodySmall,
            modifier = Modifier.weight(1f),
        )
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
