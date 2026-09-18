package io.github.rclsilver.home_genie.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
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
import androidx.compose.ui.Alignment
import androidx.compose.ui.draw.clip
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay
import io.github.rclsilver.home_genie.net.AlertPayload
import io.github.rclsilver.home_genie.net.fetchAlertHistory
import io.github.rclsilver.home_genie.net.fetchAlertsFiltered
import io.github.rclsilver.home_genie.net.fetchTopAlertLabels
import io.github.rclsilver.home_genie.net.HistoryBucketPayload
import io.github.rclsilver.home_genie.net.LabelCountPayload
import io.github.rclsilver.home_genie.service.ConnectionService

/**
 * The state of the house on one screen.
 *
 * The question asked when opening the application is "is anything waiting
 * for me", and the answer fits in three numbers. The details are one tap
 * away, behind each of them.
 */
@Composable
fun DashboardScreen(
    serverUrl: String,
    token: String,
    unread: Int,
    onOpen: (Destination) -> Unit,
) {
    var open by remember { mutableStateOf<List<AlertPayload>>(emptyList()) }
    var history by remember { mutableStateOf<List<HistoryBucketPayload>>(emptyList()) }
    var ranked by remember { mutableStateOf<List<LabelCountPayload>>(emptyList()) }
    var error by remember { mutableStateOf("") }

    val state by ConnectionService.observedState.collectAsState()
    LaunchedEffect(serverUrl, token, state.events) {
        fetchAlertsFiltered(serverUrl, token, openOnly = true)
            .onSuccess { open = it; error = "" }
            .onFailure { error = it.message ?: "loading failed" }
        // Both counted by the server. Derived here they would have been read
        // off a capped listing, and would have started lying during the very
        // storm that makes anyone open this screen.
        fetchAlertHistory(serverUrl, token, hours = HISTORY_HOURS).onSuccess { history = it }
        fetchTopAlertLabels(serverUrl, token, hours = HISTORY_HOURS).onSuccess { ranked = it }
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

    // Nothing while the socket holds. A "Connected" line shown permanently
    // stops being read after two days, and that is exactly the day it would
    // say something else. So only the silence is spoken of: with no socket,
    // no alert arrives, and the absence of alerts looks like calm.
    //
    // After a few seconds only: at launch the socket is not open yet, and
    // announcing an outage while it is being established would be a
    // one-second lie repeated on every opening.
    val down = !state.running || !state.connected
    var announce by remember { mutableStateOf(false) }
    LaunchedEffect(down) {
        if (!down) {
            announce = false
        } else {
            delay(5_000)
            announce = true
        }
    }

    if (announce) {
        Card(
            modifier = Modifier.fillMaxWidth(),
            colors = CardDefaults.cardColors(
                containerColor = MaterialTheme.colorScheme.errorContainer,
            ),
        ) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Text(
                    if (!state.running) "Service stopped" else "Reconnecting",
                    style = MaterialTheme.typography.titleSmall,
                )
                Text(
                    "While the socket is down nothing arrives — and the silence " +
                        "looks like calm.",
                    style = MaterialTheme.typography.bodySmall,
                )
            }
        }
    }

    // What needs a system setting shows here, where one looks first, and
    // nowhere at all when everything is in order.
    ReliabilityBanner(state)

    // Everything above answers "is anything waiting for me". What follows
    // answers "how has it been going", which is a different question and is
    // asked less often — so it comes after, not instead.
    if (history.any { it.total > 0 }) {
        Text("Alerts over time", style = MaterialTheme.typography.titleMedium)
        Card(Modifier.fillMaxWidth()) {
            Column(Modifier.padding(16.dp)) {
                AlertHistoryChart(history, hours = HISTORY_HOURS)
            }
        }
    }

    if (ranked.isNotEmpty()) {
        // Not "top channels", as the mockup has it. On a server where every
        // alert arrives through one channel that panel is a single row saying
        // nothing; what differs between two alerts here is the service.
        Text("What breaks most", style = MaterialTheme.typography.titleMedium)
        Card(Modifier.fillMaxWidth()) {
            Column(
                Modifier.padding(16.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                val loudest = ranked.maxOf { it.total }
                ranked.forEach { entry ->
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.spacedBy(12.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text(
                            entry.value,
                            style = MaterialTheme.typography.bodyMedium,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                            modifier = Modifier.weight(1f),
                        )
                        // A bar as well as the number: a ranking is read for
                        // its shape, and five figures in a column have none.
                        //
                        // Drawn inside a track of fixed width, so every bar
                        // starts at the same edge. Sized by its fraction of
                        // the row instead, they grew leftwards from the right
                        // and there was no common baseline to compare from.
                        Box(
                            Modifier
                                .width(120.dp)
                                .height(6.dp)
                                .clip(RoundedCornerShape(3.dp))
                                .background(MaterialTheme.colorScheme.surfaceVariant)
                        ) {
                            Box(
                                Modifier
                                    .fillMaxWidth(entry.total.toFloat() / loudest)
                                    .fillMaxHeight()
                                    .clip(RoundedCornerShape(3.dp))
                                    .background(MaterialTheme.colorScheme.primary)
                            )
                        }
                        Text(
                            entry.total.toString(),
                            style = MaterialTheme.typography.labelMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
            }
        }
    }

    // Said only when there is nothing else to say. A dashboard whose ordinary
    // state is "nothing open" cannot answer with three zeros and two blank
    // cards: silence has to be stated to be distinguished from a screen that
    // failed to load.
    if (open.isEmpty() && history.none { it.total > 0 } && error.isEmpty()) {
        Text(
            "Nothing open, and nothing in the last ${HISTORY_HOURS / 24} days.",
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

/**
 * The window the chart and the ranking cover.
 *
 * A week rather than the mockup's twelve hours: a homelab where nothing broke
 * today is the ordinary case, and a chart that is empty whenever things go
 * well is a chart nobody learns to read.
 */
private const val HISTORY_HOURS = 7 * 24
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
