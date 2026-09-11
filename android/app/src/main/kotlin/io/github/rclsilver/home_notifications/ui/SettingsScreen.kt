package io.github.rclsilver.home_notifications.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Card
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
import io.github.rclsilver.home_notifications.data.Settings
import io.github.rclsilver.home_notifications.service.ConnectionService

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
    val state by ConnectionService.observedState.collectAsState()

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
