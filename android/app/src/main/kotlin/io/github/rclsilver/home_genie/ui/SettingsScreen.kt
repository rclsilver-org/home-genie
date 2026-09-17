package io.github.rclsilver.home_genie.ui

import androidx.compose.foundation.layout.Arrangement
import java.time.temporal.ChronoUnit
import java.time.Instant
import androidx.compose.runtime.setValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Card
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import io.github.rclsilver.home_genie.data.Settings
import io.github.rclsilver.home_genie.net.fetchMute
import io.github.rclsilver.home_genie.net.setMute
import io.github.rclsilver.home_genie.service.ConnectionService

/**
 * What can be set: the session, and the system settings the application
 * depends on.
 *
 * Kept apart from the diagnostics, which only report — the two looked alike as
 * long as they shared a screen, when one is changed and the other is read.
 */
@Composable
fun SettingsScreen(settings: Settings) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val serverUrl by settings.serverUrl.collectAsState(initial = "")
    val username by settings.username.collectAsState(initial = "")
    val token by settings.token.collectAsState(initial = "")
    val state by ConnectionService.observedState.collectAsState()
    val theme by settings.theme.collectAsState(initial = "")

    Text("Appearance", style = MaterialTheme.typography.titleMedium)
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text("Theme", style = MaterialTheme.typography.titleSmall)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                val current = ThemeChoice.of(theme)
                ThemeChoice.entries.forEach { candidate ->
                    FilterChip(
                        selected = candidate == current,
                        onClick = { scope.launch { settings.saveTheme(candidate.name) } },
                        label = { Text(candidate.label) },
                    )
                }
            }
            // "System" is the default because it is the answer that keeps
            // being right: the phone already knows about the night.
            Text(
                "System follows the phone, which already switches at dusk.",
                style = MaterialTheme.typography.bodySmall,
            )
        }
    }

    Text("Account", style = MaterialTheme.typography.titleMedium)
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(username.ifEmpty { "—" }, style = MaterialTheme.typography.titleSmall)
            Text(serverUrl, style = MaterialTheme.typography.bodySmall)
        }
    }
    // The address is picked at sign-in, not here: the token belongs to the
    // server that issued it, so changing it without signing in again would
    // only give a mute session against a server that refuses it.
    Text(
        "The server address is picked at sign-in: the token is only valid on " +
            "the server that issued it.",
        style = MaterialTheme.typography.bodySmall,
    )

    MuteSection(serverUrl, token)

    QuietHoursSection(channelId = null, serverUrl = serverUrl, token = token)

    Text("System settings", style = MaterialTheme.typography.titleMedium)
    // Shown here whatever the symptom: this is the screen one comes to in
    // order to check, so what no API can verify is shown too.
    ReliabilityChecks(context, symptom = true)

    Text("Session", style = MaterialTheme.typography.titleMedium)
    TextButton(onClick = {
        scope.launch {
            ConnectionService.stop(context)
            settings.clear()
        }
    }) { Text("Sign out") }

    if (!state.running) {
        Text(
            "The service is stopped: it restarts the next time the application " +
                "is opened, or from the diagnostics.",
            style = MaterialTheme.typography.bodySmall,
        )
    }
}

/**
 * My mute: every channel at once, and me alone.
 *
 * While muted, nothing notifies me any more — not even a critical alert. It is
 * a deliberate, time-bounded gesture: "be quiet, I am the one making the
 * noise". Being woken by the rack one is working on is the most useless alert
 * there is.
 *
 * It silences nobody else: the other members keep being notified normally, and
 * do not even see that I went quiet.
 *
 * Not to be confused with quiet hours, which only turn the sound down
 * according to the clock: a mute suppresses the notification outright. The
 * messages still arrive, and stay unread.
 */
@Composable
private fun MuteSection(serverUrl: String, token: String) {
    val scope = rememberCoroutineScope()
    var mutedUntil by remember { mutableStateOf<String?>(null) }
    var error by remember { mutableStateOf("") }

    // The state follows the events: a mute set from a notification or from
    // another device must show up here without reopening the screen.
    val state by ConnectionService.observedState.collectAsState()
    LaunchedEffect(serverUrl, token, state.events) {
        if (serverUrl.isEmpty() || token.isEmpty()) return@LaunchedEffect
        fetchMute(serverUrl, token).onSuccess { mutedUntil = it.mutedUntil }
    }

    Text("Mute", style = MaterialTheme.typography.titleMedium)
    Text(
        mutedUntil?.let {
            "Nothing will notify you until ${localTime(it)} — the other " +
                "members, yes."
        } ?: "No mute. Everything notifies you normally.",
        style = MaterialTheme.typography.bodySmall,
    )
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        listOf(1L, 4L, 24L).forEach { hours ->
            TextButton(onClick = {
                scope.launch {
                    val until = Instant.now().plus(hours, ChronoUnit.HOURS)
                        .truncatedTo(ChronoUnit.SECONDS)
                    setMute(serverUrl, token, until.toString())
                        .onSuccess { mutedUntil = until.toString(); error = "" }
                        .onFailure { error = it.message ?: "failed" }
                }
            }) { Text("${hours}h") }
        }
        // Nothing to lift when nothing is set.
        if (mutedUntil != null) {
            TextButton(onClick = {
                scope.launch {
                    setMute(serverUrl, token, "")
                        .onSuccess { mutedUntil = null; error = "" }
                        .onFailure { error = it.message ?: "failed" }
                }
            }) { Text("Unmute") }
        }
    }
    if (error.isNotEmpty()) {
        Text(error, color = MaterialTheme.colorScheme.error,
            style = MaterialTheme.typography.bodySmall)
    }
}

/** A readable local time, from an RFC3339 instant. */
private fun localTime(rfc3339: String): String = runCatching {
    java.time.format.DateTimeFormatter.ofPattern("HH:mm")
        .withZone(java.time.ZoneId.systemDefault())
        .format(Instant.parse(rfc3339))
}.getOrElse { rfc3339 }
