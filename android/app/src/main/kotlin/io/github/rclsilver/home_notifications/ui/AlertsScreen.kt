package io.github.rclsilver.home_notifications.ui

import androidx.compose.foundation.BorderStroke
import androidx.compose.animation.core.Animatable
import androidx.compose.foundation.gestures.detectHorizontalDragGestures
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
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
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.IntOffset
import io.github.rclsilver.home_notifications.net.ackAlert
import io.github.rclsilver.home_notifications.net.AlertPayload
import io.github.rclsilver.home_notifications.net.fetchAlertsFiltered
import io.github.rclsilver.home_notifications.net.unackAlert
import io.github.rclsilver.home_notifications.service.ConnectionService
import kotlin.math.roundToInt
import kotlinx.coroutines.launch

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
    // The row whose drawer is open, if there is one.
    var revealed by remember { mutableStateOf<Long?>(null) }

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

    // The order is the server's: by opening date, most recent first — and by
    // resolution date in the history. Nothing rises above the rest: a list
    // whose order changes when one acknowledges makes the rows jump under the
    // thumb, and the one being looked at is lost.

    alerts.forEach { alert ->
        // Keyed by alert: without it, an alert leaving the list would
        // bequeath its open drawer to whichever takes its place.
        key(alert.id) {
            AlertRow(
                alert = alert,
                // One drawer open at a time: two gaping rows make one lose
                // track of which one was about to be acknowledged.
                revealed = revealed == alert.id,
                onReveal = { revealed = if (it) alert.id else null },
                onOpen = { revealed = null; onOpen(alert) },
                onAck = {
                    revealed = null
                    scope.launch { ackAlert(serverUrl, token, alert.id).onSuccess { reloads++ } }
                },
                onUnack = {
                    revealed = null
                    scope.launch {
                        unackAlert(serverUrl, token, alert.id).onSuccess { reloads++ }
                    }
                },
            )
        }
    }
}

/** The height shared by every row. */
private val ROW_HEIGHT = 116.dp

/** The width of the drawer revealed by the swipe. */
private val ACTION_WIDTH = 132.dp

/**
 * One alert in the console.
 *
 * Every row has the same height: a list whose rows rise and fall with the
 * length of each summary is read badly, and at three in the morning one
 * counts alerts at a glance without reading them. What does not fit is cut
 * — the detail is one tap away.
 *
 * The buttons are not in the row: they appear when it is swiped to the
 * left. They took permanent space for a rare gesture, and worse, they
 * shifted everything below depending on whether the alert was taken.
 */
@Composable
private fun AlertRow(
    alert: AlertPayload,
    revealed: Boolean,
    onReveal: (Boolean) -> Unit,
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

    // A resolved alert has no drawer: there is nothing left to do to it.
    val actionable = alert.isOpen
    val drag = rememberCoroutineScope()
    val density = LocalDensity.current
    val openOffset = with(density) { -ACTION_WIDTH.toPx() }
    val offset = remember { Animatable(0f) }

    LaunchedEffect(revealed, actionable) {
        offset.animateTo(if (revealed && actionable) openOffset else 0f)
    }

    Box(
        Modifier
            .fillMaxWidth()
            .height(ROW_HEIGHT)
    ) {
        if (actionable) {
            Box(
                modifier = Modifier
                    .align(Alignment.CenterEnd)
                    .width(ACTION_WIDTH)
                    .fillMaxHeight(),
                contentAlignment = Alignment.Center,
            ) {
                if (alert.isAcked) {
                    TextButton(onClick = onUnack) { Text("Un-acknowledge") }
                } else {
                    TextButton(onClick = onAck) { Text("Acknowledge") }
                }
            }
        }

        Card(
            modifier = Modifier
                .fillMaxSize()
                .offset { IntOffset(offset.value.roundToInt(), 0) }
                .pointerInput(actionable) {
                    if (!actionable) return@pointerInput
                    detectHorizontalDragGestures(
                        onHorizontalDrag = { change, delta ->
                            change.consume()
                            // Follow the finger without animation: a drawer
                            // trailing behind the hand does not feel pulled,
                            // it feels broken.
                            drag.launch {
                                offset.snapTo(
                                    (offset.value + delta).coerceIn(openOffset, 0f)
                                )
                            }
                        },
                        onDragEnd = {
                            val open = offset.value < openOffset / 2
                            onReveal(open)
                            // Finish the gesture here and not only by telling
                            // the parent: releasing without changing state
                            // would leave the card where the finger left it.
                            drag.launch {
                                offset.animateTo(if (open) openOffset else 0f)
                            }
                        },
                    )
                },
            colors = CardDefaults.cardColors(containerColor = container),
            // A line, the same colour on every row: it draws where the row
            // ends, not one more signal. The state is read from the badge and
            // the fill; a border changing along with them would only
            // repeat it.
            border = BorderStroke(1.dp, MaterialTheme.colorScheme.outline),
            onClick = { if (revealed) onReveal(false) else onOpen() },
        ) {
            Column(
                Modifier.padding(16.dp),
                verticalArrangement = Arrangement.spacedBy(6.dp),
            ) {
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
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )

                Text(
                    buildString {
                        append(relativeAge(alert.startedAt))
                        append(" · ").append(alert.channelSlug)
                        alert.labels["instance"]?.let { append(" · ").append(it) }
                        // "nobody" rather than a blank: the absence of an owner
                        // is the information, not a missing field.
                        append(" · ").append(if (alert.isAcked) alert.ackedBy else "nobody")
                    },
                    style = MaterialTheme.typography.bodySmall,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
    }
}
