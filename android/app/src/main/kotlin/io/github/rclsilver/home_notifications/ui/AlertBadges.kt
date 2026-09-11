package io.github.rclsilver.home_notifications.ui

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
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
import io.github.rclsilver.home_notifications.net.AlertPayload

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
        alert.isAcked -> FilledBadge("TAKEN", MaterialTheme.colorScheme.secondary)
        else -> FilledBadge("OPEN", MaterialTheme.colorScheme.error)
    }
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
    val colour = when (severity) {
        "critical" -> MaterialTheme.colorScheme.error
        "warning" -> MaterialTheme.colorScheme.tertiary
        else -> MaterialTheme.colorScheme.outline
    }
    OutlinedBadge(severity.uppercase(), colour)
}

/** "x4": the number of deliveries from Alertmanager. */
@Composable
fun OccurrencesBadge(occurrences: Int) {
    if (occurrences <= 1) return
    OutlinedBadge("×$occurrences", MaterialTheme.colorScheme.outline)
}

@Composable
private fun FilledBadge(text: String, colour: Color) {
    Text(
        text = text,
        color = MaterialTheme.colorScheme.onError,
        style = MaterialTheme.typography.labelSmall,
        fontWeight = FontWeight.Bold,
        fontSize = 10.sp,
        modifier = Modifier
            .background(colour, RoundedCornerShape(4.dp))
            .padding(horizontal = 6.dp, vertical = 2.dp),
    )
}

@Composable
private fun OutlinedBadge(text: String, colour: Color = MaterialTheme.colorScheme.outline) {
    Text(
        text = text,
        color = colour,
        style = MaterialTheme.typography.labelSmall,
        fontSize = 10.sp,
        modifier = Modifier
            .border(BorderStroke(1.dp, colour), RoundedCornerShape(4.dp))
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
