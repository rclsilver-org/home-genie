package io.github.rclsilver.home_genie.ui

import androidx.compose.foundation.background
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
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
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
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.RectangleShape
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.IntOffset
import io.github.rclsilver.home_genie.net.ackAlert
import io.github.rclsilver.home_genie.net.AlertPayload
import io.github.rclsilver.home_genie.net.fetchAlertsFiltered
import io.github.rclsilver.home_genie.net.unackAlert
import io.github.rclsilver.home_genie.service.ConnectionService
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
 * How many alerts the screen holds at once.
 *
 * The counts shown on the chips are computed here, because the server has no
 * endpoint that returns them. That is only honest while the whole set is in
 * hand: the server caps this list, so once the cap is reached a count is a
 * floor and the chip says so rather than quietly stopping to grow.
 */
private const val LIST_LIMIT = 200

/**
 * The alert console — the home screen and the main function of the
 * application: knowing what is open, who has taken it, and acknowledging it.
 */
@Composable
fun AlertsScreen(serverUrl: String, token: String, onOpen: (AlertPayload) -> Unit) {
    val scope = rememberCoroutineScope()
    var filter by remember { mutableStateOf(AlertFilter.OPEN) }
    var all by remember { mutableStateOf<List<AlertPayload>>(emptyList()) }
    var capped by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf("") }
    var reloads by remember { mutableStateOf(0) }
    // The row whose drawer is open, if there is one.
    var revealed by remember { mutableStateOf<Long?>(null) }

    val state by ConnectionService.observedState.collectAsState()
    // One fetch for every tab, where there used to be one per tab. Switching
    // is instant, and each tab can say how much it holds without a round trip
    // nobody would wait for.
    LaunchedEffect(state.events, reloads) {
        fetchAlertsFiltered(serverUrl, token, limit = LIST_LIMIT)
            .onSuccess { all = it; capped = it.size >= LIST_LIMIT; error = "" }
            .onFailure { error = it.message ?: "loading failed" }
    }

    fun matching(candidate: AlertFilter) = when (candidate) {
        AlertFilter.OPEN -> all.filter { it.isOpen }
        AlertFilter.CRITICAL -> all.filter { it.isOpen && it.severity == "critical" }
        AlertFilter.CLOSED -> all.filter { !it.isOpen }
        AlertFilter.ALL -> all
    }

    Row(
        modifier = Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        AlertFilter.entries.forEach { candidate ->
            val count = matching(candidate).size
            FilterChip(
                selected = candidate == filter,
                onClick = { filter = candidate },
                // The count where the eye already is: "Open 8" answers the
                // screen's question before the list below is even read.
                label = { Text("${candidate.label}  ${if (capped) "$count+" else "$count"}") },
            )
        }
    }

    if (error.isNotEmpty()) {
        Text(error, color = MaterialTheme.colorScheme.error)
    }

    val alerts = matching(filter)
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

    // Computed once for the whole list: a row cannot tell on its own whether
    // what it is about to print also appears on the nineteen below it.
    val facets = facetsOf(alerts)

    alerts.forEach { alert ->
        // Keyed by alert: without it, an alert leaving the list would
        // bequeath its open drawer to whichever takes its place.
        key(alert.id) {
            AlertRow(
                alert = alert,
                facets = facets,
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

/**
 * The width of the drawer revealed by the swipe.
 *
 * Sized for a real button rather than a line of text. A tappable label with
 * no edges reads as a link, and a link in a row one has just dragged open
 * does not look like the thing that was being reached for.
 */
private val ACTION_WIDTH = 94.dp

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
    facets: Facets,
    revealed: Boolean,
    onReveal: (Boolean) -> Unit,
    onOpen: () -> Unit,
    onAck: () -> Unit,
    onUnack: () -> Unit,
) {
    // A resolved alert has no drawer: there is nothing left to do to it.
    val actionable = alert.isOpen
    val drag = rememberCoroutineScope()
    val density = LocalDensity.current
    val openOffset = with(density) { -ACTION_WIDTH.toPx() }
    val offset = remember { Animatable(0f) }

    LaunchedEffect(revealed, actionable) {
        offset.animateTo(if (revealed && actionable) openOffset else 0f)
    }

    // A resolved alert keeps its stripe but loses its colour: the severity was
    // true while it was running and says nothing about it now.
    val stripe = if (alert.isOpen) severityColour(alert.severity)
    else MaterialTheme.colorScheme.outline
    val outline = MaterialTheme.colorScheme.outline

    // The height follows the content, and therefore the reader's font size.
    // It was pinned at 94dp, measured at a scale of 1.0; at 1.15 — which is
    // what a Samsung ships with — the three lines no longer fit and the tags
    // were sliced in half. Every line here is already capped at one, so the
    // rows stay as even as the fixed height made them, without ignoring a
    // setting somebody chose in order to be able to read.
    Box(
        Modifier
            .fillMaxWidth()
            .height(IntrinsicSize.Min)
            // The rounded corners and the outline belong to the row, not to
            // the card sliding inside it. Giving each of them its own shape
            // meant two shapes had to agree on where the row ended, and for
            // the first pixels of a drag they never quite did: corners popped
            // square, a hairline seam appeared, the panel showed through.
            // Clipped once here, nothing inside has to know.
            .clip(ROW_SHAPE)
            .drawWithContent {
                drawContent()
                // Drawn after the children, so the card cannot cover it, and
                // always the same stroke whatever is moving underneath.
                val w = 1.dp.toPx()
                drawRoundRect(
                    color = outline,
                    topLeft = Offset(w / 2, w / 2),
                    size = Size(size.width - w, size.height - w),
                    cornerRadius = CornerRadius(ROW_RADIUS.toPx()),
                    style = Stroke(w),
                )
            }
    ) {
        if (actionable) {
            // Square: the parent clip rounds whatever reaches the row's edge,
            // so the panel never has to guess which of its corners show.
            Surface(
                onClick = if (alert.isAcked) onUnack else onAck,
                modifier = Modifier
                    .align(Alignment.CenterEnd)
                    .width(ACTION_WIDTH)
                    .fillMaxHeight(),
                shape = RectangleShape,
                // The same grey as the TAKEN badge: once an alert is taken,
                // that colour is what says so, here as on the row. Taking is
                // the offer, undoing a way back — one calls, the other waits.
                color = if (alert.isAcked) MaterialTheme.colorScheme.secondary
                else MaterialTheme.colorScheme.primary,
            ) {
                Box(contentAlignment = Alignment.Center) {
                    Text(
                        if (alert.isAcked) "Unack" else "Ack",
                        style = MaterialTheme.typography.labelLarge,
                        color = if (alert.isAcked) MaterialTheme.colorScheme.onSecondary
                        else MaterialTheme.colorScheme.onPrimary,
                    )
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
            colors = CardDefaults.cardColors(
                containerColor = MaterialTheme.colorScheme.surface,
            ),
            // Square, and with no border of its own. Both are the row's job.
            shape = RectangleShape,
            onClick = { if (revealed) onReveal(false) else onOpen() },
        ) {
            Row(Modifier.fillMaxWidth().height(IntrinsicSize.Min)) {
                // Severity as a stripe rather than a fill. Eight open alerts
                // painted the whole screen red, and a list where every row
                // shouts ranks nothing — which is the one thing this screen
                // exists to do.
                Box(
                    Modifier
                        .width(4.dp)
                        .fillMaxHeight()
                        .background(stripe)
                )
                Column(
                    Modifier.padding(14.dp),
                    verticalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        // The title first and widest. It is what one actually
                        // reads, and a row of badges ahead of it pushed it to
                        // a second glance.
                        Text(
                            alert.title,
                            style = MaterialTheme.typography.titleSmall,
                            fontWeight = if (alert.isOpen && !alert.isAcked) FontWeight.Bold
                            else FontWeight.Normal,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                            modifier = Modifier.weight(1f),
                        )
                        StatusBadge(alert)
                    }

                    Text(
                        buildString {
                            // Only what differs from the rows around it. On a
                            // server with one alert channel the slug was the
                            // same word twenty times over.
                            if (facets.channel) append(alert.channelSlug).append(" · ")
                            append(relativeAge(alert.startedAt))
                            alert.labels["instance"]
                                ?.removeSuffix(facets.sharedDomain)
                                ?.let { append(" · ").append(it) }
                            // "nobody" rather than a blank: the absence of an
                            // owner is the information, not a missing field.
                            append(" · ").append(if (alert.isAcked) alert.ackedBy else "nobody")
                        },
                        style = MaterialTheme.typography.bodySmall,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )

                    Row(
                        horizontalArrangement = Arrangement.spacedBy(4.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        // No severity pill here: the stripe down the side
                        // already says it, in the same colour, a centimetre
                        // away. The word itself is on the detail screen,
                        // where there is room to be precise.
                        LabelChips(alert.labels, facets.labels, max = 3)
                        OccurrencesBadge(alert.occurrences)
                    }
                }
            }
        }
    }
}
