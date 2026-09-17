package io.github.rclsilver.home_genie.ui

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import io.github.rclsilver.home_genie.net.AlertPayload

/**
 * An alert's badges.
 *
 * Filled while the alert demands something, outlined when it no longer
 * does: a resolved alert must be visible without shouting, and a screen
 * full of bright pills ends up signalling nothing.
 */
@Composable
fun StatusBadge(alert: AlertPayload) {
    when {
        !alert.isOpen -> OutlinedBadge("RESOLVED")
        alert.isAcked -> FilledBadge(
            "TAKEN",
            MaterialTheme.colorScheme.secondary,
            MaterialTheme.colorScheme.onSecondary,
        )
        else -> FilledBadge(
            "OPEN",
            MaterialTheme.colorScheme.error,
            MaterialTheme.colorScheme.onError,
        )
    }
}

/**
 * The colour a severity is entitled to, shared by the badge and the stripe
 * down the side of a row so the two can never disagree.
 */
@Composable
fun severityColour(severity: String): Color = when (severity) {
    "critical" -> MaterialTheme.colorScheme.error
    "warning" -> MaterialTheme.colorScheme.tertiary
    else -> MaterialTheme.colorScheme.outline
}

/**
 * The Alertmanager severity as it is, not translated into P1–P5.
 *
 * It is the vocabulary of the Prometheus rules and of the monitoring stack;
 * a parallel scale would force a mental translation on every read.
 */
@Composable
fun SeverityBadge(severity: String) {
    if (severity.isEmpty()) return
    // `info` gets no colour of its own on the badge: it is the absence of a
    // signal, and painting it would put three competing hues on one row.
    val colour = if (severity == "critical" || severity == "warning") {
        severityColour(severity)
    } else null
    OutlinedBadge(severity.uppercase(), colour)
}

/**
 * The labels already spoken for elsewhere on a row: the alert name is the
 * title, the severity has its own badge, the instance sits on the meta line,
 * and `job` is not something anyone acts on.
 */
private val SPOKEN_FOR = setOf("alertname", "severity", "instance", "job")

/**
 * What an alert carries beyond the usual labels — `service`, `source`, and
 * whatever the rule chose to attach. That is the part which differs between
 * two alerts sharing a name, so it is the part worth the width.
 */
@Composable
fun LabelChips(labels: Map<String, String>, keys: Set<String>, max: Int = 3) {
    val extra = labels
        .filterKeys { it !in SPOKEN_FOR && it in keys }
        .entries.sortedBy { it.key }
        .take(max)
    if (extra.isEmpty()) return
    Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
        extra.forEach { OutlinedBadge(it.value) }
    }
}

/** "x4": the number of deliveries from Alertmanager. */
@Composable
fun OccurrencesBadge(occurrences: Int) {
    if (occurrences <= 1) return
    OutlinedBadge("×$occurrences")
}

@Composable
// The content colour travels with the fill rather than being assumed: the
// badge is drawn on `error` in one case and on `secondary` in the other, and
// a single hardcoded foreground only happened to be legible on both.
private fun FilledBadge(text: String, colour: Color, onColour: Color) {
    Text(
        text = text,
        color = onColour,
        style = MaterialTheme.typography.labelSmall,
        fontWeight = FontWeight.Bold,
        fontSize = 10.sp,
        modifier = Modifier
            .background(colour, RoundedCornerShape(4.dp))
            .padding(horizontal = 6.dp, vertical = 2.dp),
    )
}

@Composable
// A severity passes its own colour and wears it on both the border and the
// text, because there the colour *is* the signal. A neutral badge does not:
// `outline` is a colour for a line, and used as text it is barely legible on
// a dark surface — the occurrence count and the labels had all but vanished.
private fun OutlinedBadge(text: String, colour: Color? = null) {
    val line = colour ?: MaterialTheme.colorScheme.outline
    Text(
        text = text,
        color = colour ?: MaterialTheme.colorScheme.onSurfaceVariant,
        style = MaterialTheme.typography.labelSmall,
        fontSize = 10.sp,
        modifier = Modifier
            .border(BorderStroke(1.dp, line), RoundedCornerShape(4.dp))
            .padding(horizontal = 6.dp, vertical = 2.dp),
    )
}

/**
 * "3 h ago" rather than a timestamp.
 *
 * It is the age that worries, and converting an hour into a duration at
 * three in the morning is a load one can avoid. Past two days the duration
 * says nothing useful any more and the date comes back.
 */
fun relativeAge(rfc3339: String): String = runCatching {
    val started = java.time.Instant.parse(rfc3339)
    val minutes = java.time.temporal.ChronoUnit.MINUTES.between(started, java.time.Instant.now())
    when {
        minutes < 1 -> "just now"
        minutes < 60 -> "${minutes} min"
        minutes < 60 * 48 -> "${minutes / 60} h"
        else -> java.time.format.DateTimeFormatter.ofPattern("dd/MM HH:mm")
            .withZone(java.time.ZoneId.systemDefault()).format(started)
    }
}.getOrElse { rfc3339 }
