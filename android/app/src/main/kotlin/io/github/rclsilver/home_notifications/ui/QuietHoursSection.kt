package io.github.rclsilver.home_notifications.ui

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import io.github.rclsilver.home_notifications.net.QuietHoursPayload
import io.github.rclsilver.home_notifications.net.fetchQuietHours
import io.github.rclsilver.home_notifications.net.setQuietHours

/** The broad window, then the ones that can name a severity. */
private val SCOPES = listOf("", "critical", "warning", "info")

/**
 * A channel's quiet hours.
 *
 * A quiet window **silences**, it does not hold back: the message arrives all
 * the same, it simply makes no noise, and an unacknowledged alert resurfaces
 * when the window closes. That is the whole difference with a mute, which
 * does lose things.
 */
@Composable
fun QuietHoursSection(channelId: Long, serverUrl: String, token: String) {
    val scope = rememberCoroutineScope()
    var windows by remember { mutableStateOf<List<QuietHoursPayload>>(emptyList()) }
    var error by remember { mutableStateOf("") }
    var reloads by remember { mutableStateOf(0) }

    // Keyed on the address and the token as well: they arrive from the store,
    // so the first composition has neither. Firing then would leave the
    // failure of a request addressed to nobody on screen, and nothing would
    // ever ask again.
    LaunchedEffect(serverUrl, token, channelId, reloads) {
        if (serverUrl.isEmpty() || token.isEmpty()) return@LaunchedEffect
        fetchQuietHours(serverUrl, token, channelId)
            .onSuccess { windows = it; error = "" }
            .onFailure { error = it.message ?: "" }
    }

    Text("Quiet hours", style = MaterialTheme.typography.titleMedium)
    Text(
        "During the window, messages arrive without noise. Nothing is lost: " +
            "an unacknowledged alert comes back at the end of the window.",
        style = MaterialTheme.typography.bodySmall,
    )
    if (error.isNotEmpty()) {
        Text(error, color = MaterialTheme.colorScheme.error)
    }

    SCOPES.forEach { severity ->
        QuietHoursCard(severity, windows.firstOrNull { it.severity == severity }) { updated ->
            scope.launch {
                setQuietHours(serverUrl, token, channelId, updated)
                    .onSuccess { error = ""; reloads++ }
                    .onFailure { error = it.message ?: "saving failed" }
            }
        }
    }
}

@Composable
private fun QuietHoursCard(
    severity: String,
    current: QuietHoursPayload?,
    onSave: (QuietHoursPayload) -> Unit,
) {
    var from by remember(current) { mutableStateOf(current?.from ?: "") }
    var to by remember(current) { mutableStateOf(current?.to ?: "") }

    // A window naming "critical" is the only one that can silence a critical
    // alert, and that is deliberate: a broad window does not cover them.
    val critical = severity == "critical"

    Card(
        modifier = Modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(
            containerColor = if (critical && from.isNotEmpty())
                MaterialTheme.colorScheme.errorContainer
            else MaterialTheme.colorScheme.surface,
        ),
        border = BorderStroke(1.dp, MaterialTheme.colorScheme.outline),
    ) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(
                if (severity.isEmpty()) "Everything" else severity,
                style = MaterialTheme.typography.titleSmall,
            )
            Text(
                when {
                    severity.isEmpty() ->
                        "Does not cover critical alerts: silencing those is asked for " +
                            "by naming 'critical'."
                    critical ->
                        "Silencing a critical is a deliberate choice — on a homelab, a " +
                            "disk filling up at night can wait until morning."
                    else -> "Replaces the channel window for this severity."
                },
                style = MaterialTheme.typography.bodySmall,
            )

            // Both bounds go together: half a window would silence at an
            // unpredictable hour, and the server refuses it. Both empty means
            // "no quiet hours".
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(
                    value = from,
                    onValueChange = { from = it },
                    label = { Text("From") },
                    placeholder = { Text("23:00") },
                    singleLine = true,
                    modifier = Modifier.width(150.dp),
                )
                OutlinedTextField(
                    value = to,
                    onValueChange = { to = it },
                    label = { Text("to") },
                    placeholder = { Text("07:00") },
                    singleLine = true,
                    modifier = Modifier.width(150.dp),
                )
            }

            // Nothing to remove when nothing exists: an enabled button that does
            // nothing teaches doubt about the ones that do something.
            val empty = from.isBlank() && to.isBlank()
            TextButton(
                enabled = !empty || current != null,
                onClick = {
                    onSave(
                        QuietHoursPayload(
                            severity = severity,
                            from = from.trim(),
                            to = to.trim(),
                        )
                    )
                },
            ) {
                Text(if (empty) "Retirer" else "Enregistrer")
            }
        }
    }
}
