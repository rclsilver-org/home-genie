package io.github.rclsilver.home_notifications.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import io.github.rclsilver.home_notifications.data.Settings
import io.github.rclsilver.home_notifications.net.ApiClient
import io.github.rclsilver.home_notifications.net.LoginRequest
import io.github.rclsilver.home_notifications.service.ConnectionService
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

/**
 * Address pre-filled on the first sign-in, purely for convenience. Port 8088
 * and not 8080: that is the one the development machine's firewall lets
 * through, and an unreachable server looks exactly like a stopped one. The
 * field stays editable.
 */
private const val DEFAULT_SERVER_URL = "http://192.0.2.10:8088"

@Composable
fun AppScreen(settings: Settings) {
    val token by settings.token.collectAsState(initial = "")

    Scaffold { padding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding)
                .padding(20.dp)
                .verticalScroll(rememberScrollState()),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            if (token.isEmpty()) {
                LoginCard(settings)
            } else {
                DiagnosticCard(settings)
            }
        }
    }
}

@Composable
private fun LoginCard(settings: Settings) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val savedUrl by settings.serverUrl.collectAsState(initial = "")

    var serverUrl by remember(savedUrl) { mutableStateOf(savedUrl.ifEmpty { DEFAULT_SERVER_URL }) }
    var username by remember { mutableStateOf("") }
    var password by remember { mutableStateOf("") }
    var error by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }

    Text("home-notifications", style = MaterialTheme.typography.headlineMedium)

    // The URL is a setting, not a constant: it is what lets one switch
    // between the development server and production.
    OutlinedTextField(
        value = serverUrl,
        onValueChange = { serverUrl = it },
        label = { Text("Server") },
        singleLine = true,
        modifier = Modifier.fillMaxWidth(),
    )
    OutlinedTextField(
        value = username,
        onValueChange = { username = it },
        label = { Text("Username") },
        singleLine = true,
        modifier = Modifier.fillMaxWidth(),
    )
    OutlinedTextField(
        value = password,
        onValueChange = { password = it },
        label = { Text("Password") },
        singleLine = true,
        visualTransformation = PasswordVisualTransformation(),
        modifier = Modifier.fillMaxWidth(),
    )

    if (error.isNotEmpty()) {
        Text(error, color = MaterialTheme.colorScheme.error)
    }

    Button(
        onClick = {
            busy = true
            error = ""
            scope.launch {
                val result = ApiClient().login(
                    serverUrl,
                    LoginRequest(
                        username = username,
                        password = password,
                        deviceName = android.os.Build.MODEL ?: "android",
                    ),
                )
                busy = false
                result.onSuccess {
                    settings.saveSession(serverUrl, it.token, it.user.username)
                    ConnectionService.start(context)
                }.onFailure {
                    error = it.message ?: "sign-in failed"
                }
            }
        },
        enabled = !busy && username.isNotBlank() && password.isNotBlank(),
        modifier = Modifier.fillMaxWidth(),
    ) {
        Text(if (busy) "Signing in…" else "Sign in")
    }
}

@Composable
private fun DiagnosticCard(settings: Settings) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val state by ConnectionService.observedState.collectAsState()
    val username by settings.username.collectAsState(initial = "")
    val persistedSeq by settings.lastSeq.collectAsState(initial = 0L)

    Text("Diagnostics", style = MaterialTheme.typography.headlineMedium)
    Text("Signed in as $username", style = MaterialTheme.typography.bodyMedium)

    Card(modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Line("Service", if (state.running) "running" else "stopped")
            Line("Socket", if (state.connected) "connected" else "disconnected")
            Line("State", state.detail)
            // The crux of the overnight test: a stale heartbeat reveals a
            // socket the system froze without closing.
            Line("Last heartbeat", timestamp(state.lastHeartbeat))
            Line("Heartbeats received", state.heartbeats.toString())
            Line("Events received", state.events.toString())
            Line("Last event", state.lastEvent.ifEmpty { "—" })
            Line("Socket failures", state.failures.toString())
            Line("Cursor (seq)", persistedSeq.toString())
            Line("Service started", timestamp(state.startedAt))
        }
    }

    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Button(onClick = { ConnectionService.start(context) }) { Text("Start") }
        OutlinedButton(onClick = { ConnectionService.stop(context) }) { Text("Stop") }
    }

    Spacer(Modifier.height(8.dp))
    Text("Reliability", style = MaterialTheme.typography.titleMedium)

    reliabilityChecks(context).forEach { check ->
        Card(modifier = Modifier.fillMaxWidth()) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Text(
                    (if (check.satisfied) "✓ " else "• ") + check.label,
                    style = MaterialTheme.typography.titleSmall,
                )
                Text(check.explanation, style = MaterialTheme.typography.bodySmall)
                check.fix?.let { intent ->
                    TextButton(onClick = { context.startActivity(intent()) }) {
                        Text("Open the setting")
                    }
                }
            }
        }
    }

    Spacer(Modifier.height(8.dp))
    TextButton(onClick = {
        scope.launch {
            ConnectionService.stop(context)
            settings.clear()
        }
    }) {
        Text("Sign out")
    }
}

@Composable
private fun Line(label: String, value: String) {
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(label, style = MaterialTheme.typography.bodySmall)
        Text(value, style = MaterialTheme.typography.bodyMedium)
    }
}

private val formatter = SimpleDateFormat("MM-dd HH:mm:ss", Locale.ROOT)

private fun timestamp(millis: Long): String =
    if (millis == 0L) "—" else formatter.format(Date(millis))
