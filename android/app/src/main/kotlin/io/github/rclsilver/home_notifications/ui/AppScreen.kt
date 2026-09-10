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
import androidx.compose.runtime.LaunchedEffect
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
import io.github.rclsilver.home_notifications.MainActivity
import io.github.rclsilver.home_notifications.data.Settings
import io.github.rclsilver.home_notifications.net.ApiClient
import io.github.rclsilver.home_notifications.net.ChannelPayload
import io.github.rclsilver.home_notifications.net.LoginRequest
import io.github.rclsilver.home_notifications.net.listChannels
import io.github.rclsilver.home_notifications.net.markChannelRead
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
    val context = LocalContext.current
    val token by settings.token.collectAsState(initial = "")

    // As soon as a session exists, the service must be running. Without this
    // an application update — which kills the service without START_STICKY
    // bringing it back — would leave the app silent while looking fine.
    // Starting an already running service is a no-op.
    LaunchedEffect(token) {
        if (token.isNotEmpty()) ConnectionService.start(context)
    }

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

    val oidcError by MainActivity.observedLoginError.collectAsState()
    if (error.isNotEmpty()) {
        Text(error, color = MaterialTheme.colorScheme.error)
    }
    oidcError?.let { Text(it, color = MaterialTheme.colorScheme.error) }

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
        Text(if (busy) "Signing in…" else "Sign in with the break-glass account")
    }

    // The identity provider day to day; the local account stays the net for
    // when whatever hosts it is unavailable.
    OutlinedButton(
        onClick = {
            error = ""
            MainActivity.clearLoginError()
            scope.launch {
                startOidcLogin(context, serverUrl).onFailure {
                    error = it.message ?: "could not start authentication"
                }
            }
        },
        enabled = !busy,
        modifier = Modifier.fillMaxWidth(),
    ) {
        Text("Sign in with the identity provider")
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
    ChannelsSection(settings)

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

/**
 * The caller's channels, with **their** unread count: a message read by one
 * member stays unread for the others.
 */
@Composable
private fun ChannelsSection(settings: Settings) {
    val scope = rememberCoroutineScope()
    val serverUrl by settings.serverUrl.collectAsState(initial = "")
    val token by settings.token.collectAsState(initial = "")
    var channels by remember { mutableStateOf<List<ChannelPayload>>(emptyList()) }
    var error by remember { mutableStateOf("") }

    // Reloaded when shown and on every event received: the counter follows
    // the incoming messages without having to be recomputed locally.
    val events by ConnectionService.observedState.collectAsState()
    LaunchedEffect(serverUrl, token, events.events) {
        if (serverUrl.isEmpty() || token.isEmpty()) return@LaunchedEffect
        listChannels(serverUrl, token)
            .onSuccess { channels = it; error = "" }
            .onFailure { error = it.message ?: "failed" }
    }

    Text("Channels", style = MaterialTheme.typography.titleMedium)
    if (error.isNotEmpty()) {
        Text(error, color = MaterialTheme.colorScheme.error)
    }
    if (channels.isEmpty() && error.isEmpty()) {
        Text("no channel", style = MaterialTheme.typography.bodySmall)
    }

    channels.forEach { channel ->
        Card(modifier = Modifier.fillMaxWidth()) {
            Row(
                modifier = Modifier.fillMaxWidth().padding(16.dp),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column {
                    Text(channel.name.ifEmpty { channel.slug },
                        style = MaterialTheme.typography.titleSmall)
                    Text("${channel.slug} · ${channel.role}",
                        style = MaterialTheme.typography.bodySmall)
                }
                if (channel.unread > 0) {
                    TextButton(onClick = {
                        scope.launch {
                            markChannelRead(serverUrl, token, channel.id)
                            listChannels(serverUrl, token)
                                .onSuccess { channels = it }
                        }
                    }) {
                        Text("${channel.unread} non lus")
                    }
                } else {
                    Text("up to date", style = MaterialTheme.typography.bodySmall)
                }
            }
        }
    }
}
