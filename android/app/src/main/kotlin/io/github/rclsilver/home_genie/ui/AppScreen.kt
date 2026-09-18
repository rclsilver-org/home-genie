package io.github.rclsilver.home_genie.ui

import android.content.Context
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.DrawerValue
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalNavigationDrawer
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.rememberDrawerState
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Menu
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import kotlinx.coroutines.launch
import io.github.rclsilver.home_genie.data.Settings
import io.github.rclsilver.home_genie.MainActivity
import io.github.rclsilver.home_genie.net.ApiClient
import io.github.rclsilver.home_genie.net.ChannelPayload
import io.github.rclsilver.home_genie.net.MessagePayload
import io.github.rclsilver.home_genie.net.markFeedRead
import io.github.rclsilver.home_genie.net.createChannel
import io.github.rclsilver.home_genie.net.CreateChannelRequest
import io.github.rclsilver.home_genie.net.deleteChannel
import io.github.rclsilver.home_genie.net.fetchUnreadFeedCount
import io.github.rclsilver.home_genie.net.listChannels
import io.github.rclsilver.home_genie.net.LoginRequest
import io.github.rclsilver.home_genie.net.markChannelRead
import io.github.rclsilver.home_genie.service.ConnectionService

/**
 * Address pre-filled on the first sign-in, purely for convenience: the field
 * stays editable, and it is what lets one switch between a development server
 * and production.
 */
private const val DEFAULT_SERVER_URL = "https://"

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AppScreen(settings: Settings) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val token by settings.token.collectAsState(initial = "")
    val serverUrl by settings.serverUrl.collectAsState(initial = "")
    val username by settings.username.collectAsState(initial = "")

    // Navigation by plain state: a handful of screens does not justify a
    // library, and what is open must survive a recomposition but not the
    // process.
    var openChannel by remember { mutableStateOf<ChannelPayload?>(null) }
    // The open alert, if any: the console and the detail are the same
    // section, not two destinations.
    var openAlert by remember { mutableStateOf<Long?>(null) }
    // The message itself and not its id: there is no endpoint to fetch one
    // back, and the list already holds it.
    var openMessage by remember { mutableStateOf<MessagePayload?>(null) }
    var destination by remember { mutableStateOf(Destination.OVERVIEW) }
    val drawerState = rememberDrawerState(DrawerValue.Closed)

    // Recounted on every event received: the server is the one that knows,
    // and keeping a parallel count would end up diverging from its own.
    var unread by remember { mutableStateOf(0) }
    val connection by ConnectionService.observedState.collectAsState()
    LaunchedEffect(serverUrl, token, connection.events, destination) {
        if (serverUrl.isEmpty() || token.isEmpty()) return@LaunchedEffect
        fetchUnreadFeedCount(serverUrl, token).onSuccess { unread = it }
    }

    // As soon as a session exists, the service must be running. Without this
    // an application update — which kills the service without START_STICKY
    // bringing it back — would leave the app silent while looking fine.
    // Starting an already running service is a no-op.
    LaunchedEffect(token) {
        if (token.isNotEmpty()) ConnectionService.start(context)
    }

    // No drawer until there is a session: there would be a single screen
    // behind it, and a menu that leads nowhere.
    if (token.isEmpty()) {
        Scaffold { padding ->
            Column(
                modifier = Modifier
                    .fillMaxSize()
                    .padding(padding)
                    .padding(20.dp)
                    .verticalScroll(rememberScrollState()),
                verticalArrangement = Arrangement.spacedBy(16.dp),
            ) {
                LoginCard(settings)
            }
        }
        return
    }

    // The system back does the same as the arrow: close what is open on top
    // of the section. Without it, back left the application from inside a
    // channel, which is never what pressing back asks for.
    BackHandler(enabled = openChannel != null || openAlert != null) {
        openChannel = null
        openAlert = null
    }

    ModalNavigationDrawer(
        drawerState = drawerState,
        drawerContent = {
            DrawerContent(destination, unread, username) { selected ->
                destination = selected
                // Going back to the menu closes what was open: otherwise one
                // would later land back on a channel believed to be left.
                openChannel = null
                openAlert = null
                openMessage = null
                scope.launch { drawerState.close() }
            }
        },
    ) {
        Scaffold(
            topBar = {
                // The title and the arrow say where one is: the drawer only
                // opens from a section, and what is open on top of it closes
                // with back rather than with a button dropped in the page.
                val openedChannel = openChannel
                val openedAlert = openAlert
                val openedMessage = openMessage
                val nested = openedChannel != null ||
                    (destination == Destination.ALERTS && openedAlert != null) ||
                    (destination == Destination.NOTIFICATIONS && openedMessage != null)
                TopAppBar(
                    title = {
                        Text(
                            when {
                                openedChannel != null -> openedChannel.slug
                                openedAlert != null && destination == Destination.ALERTS ->
                                    "Alert #$openedAlert"
                                openedMessage != null &&
                                    destination == Destination.NOTIFICATIONS -> "Notification"
                                else -> destination.label
                            }
                        )
                    },
                    actions = {
                        // Only on the feed, and not on a message opened from it:
                        // an action that empties the whole list has no place
                        // above one of its rows.
                        if (destination == Destination.NOTIFICATIONS && openedMessage == null) {
                            var menuOpen by remember { mutableStateOf(false) }
                            IconButton(onClick = { menuOpen = true }) {
                                Icon(Icons.Default.MoreVert, contentDescription = "More")
                            }
                            DropdownMenu(
                                expanded = menuOpen,
                                onDismissRequest = { menuOpen = false },
                            ) {
                                DropdownMenuItem(
                                    text = { Text("Mark all read") },
                                    // Nothing to mark is not something to hide:
                                    // a button that comes and goes makes the bar
                                    // jump, and a menu that opens onto nothing
                                    // is worse than one offering a grey line.
                                    enabled = unread > 0,
                                    onClick = {
                                        menuOpen = false
                                        // No local reload: the server tells this
                                        // user's devices, and this one is among
                                        // them, so the feed refreshes down the
                                        // same path that syncs the others.
                                        scope.launch { markFeedRead(serverUrl, token) }
                                    },
                                )
                            }
                        }
                    },
                    navigationIcon = {
                        if (nested) {
                            IconButton(onClick = {
                                openChannel = null; openAlert = null; openMessage = null
                            }) {
                                Icon(
                                    Icons.AutoMirrored.Filled.ArrowBack,
                                    contentDescription = "Back",
                                )
                            }
                        } else {
                            IconButton(onClick = { scope.launch { drawerState.open() } }) {
                                Icon(Icons.Default.Menu, contentDescription = "Menu")
                            }
                        }
                    },
                )
            },
        ) { padding ->
            Column(
                modifier = Modifier
                    .fillMaxSize()
                    .padding(padding)
                    .padding(20.dp)
                    .verticalScroll(rememberScrollState()),
                verticalArrangement = Arrangement.spacedBy(16.dp),
            ) {
                val current = openChannel
                when {
                    current != null -> ChannelScreen(
                        channel = current,
                        serverUrl = serverUrl,
                        token = token,
                    )
                    destination == Destination.OVERVIEW -> OverviewScreen(
                        serverUrl = serverUrl,
                        token = token,
                        unread = unread,
                        onOpen = { destination = it },
                    )
                    destination == Destination.ALERTS -> {
                        val opened = openAlert
                        if (opened == null) {
                            AlertsScreen(serverUrl, token) { openAlert = it.id }
                        } else {
                            AlertDetailScreen(serverUrl, token, opened)
                        }
                    }
                    destination == Destination.NOTIFICATIONS -> {
                        val opened = openMessage
                        if (opened == null) {
                            NotificationsScreen(serverUrl, token) { openMessage = it }
                        } else {
                            NotificationDetailScreen(opened, serverUrl, token)
                        }
                    }
                    destination == Destination.CHANNELS ->
                        ChannelsScreen(settings) { openChannel = it }
                    destination == Destination.SETTINGS -> SettingsScreen(settings)
                    destination == Destination.DIAGNOSTICS -> DiagnosticCard(settings)
                }
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

    Text("Home Genie", style = MaterialTheme.typography.headlineMedium)

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


    ReliabilityBanner(state)


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
private fun ChannelsSection(settings: Settings, onOpenChannel: (ChannelPayload) -> Unit) {
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

    NewChannelRow { slug ->
        scope.launch {
            createChannel(serverUrl, token, CreateChannelRequest(slug = slug))
                .onSuccess {
                    error = ""
                    listChannels(serverUrl, token).onSuccess { channels = it }
                }
                .onFailure { error = it.message ?: "creation failed" }
        }
    }
    if (error.isNotEmpty()) {
        Text(error, color = MaterialTheme.colorScheme.error)
    }
    if (channels.isEmpty() && error.isEmpty()) {
        Text("no channel", style = MaterialTheme.typography.bodySmall)
    }

    // The channel whose deletion was asked for, awaiting confirmation.
    var pendingDelete by remember { mutableStateOf<ChannelPayload?>(null) }

    channels.forEach { channel ->
        Card(
            modifier = Modifier.fillMaxWidth(),
            border = BorderStroke(1.dp, MaterialTheme.colorScheme.outline),
            onClick = { onOpenChannel(channel) },
        ) {
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
                Row(verticalAlignment = Alignment.CenterVertically) {
                    if (channel.unread > 0) {
                        TextButton(onClick = {
                            scope.launch {
                                markChannelRead(serverUrl, token, channel.id)
                                listChannels(serverUrl, token)
                                    .onSuccess { channels = it }
                            }
                        }) {
                            Text("${channel.unread} unread")
                        }
                    }
                    // Only an owner can delete, and the server checks it
                    // anyway: offering the gesture to someone who cannot do
                    // it would only produce a refusal.
                    if (channel.role == "owner") {
                        IconButton(onClick = { pendingDelete = channel }) {
                            Icon(
                                Icons.Default.Delete,
                                contentDescription = "Delete ${channel.slug}",
                            )
                        }
                    }
                }
            }
        }
    }

    // A confirmation that says what disappears, and names the channel. A
    // deletion takes the messages, the alerts and the tokens with it: it is
    // the only gesture in this application that destroys something for
    // everyone, and nothing undoes it.
    pendingDelete?.let { doomed ->
        AlertDialog(
            onDismissRequest = { pendingDelete = null },
            title = { Text("Delete '${doomed.slug}'?") },
            text = {
                Text(
                    "Its messages, its alerts, its members and its publish " +
                        "tokens go with it. Producers that published there " +
                        "will get an error. This is permanent."
                )
            },
            confirmButton = {
                TextButton(onClick = {
                    pendingDelete = null
                    scope.launch {
                        deleteChannel(serverUrl, token, doomed.id)
                            .onSuccess {
                                error = ""
                                listChannels(serverUrl, token).onSuccess { channels = it }
                            }
                            .onFailure { error = it.message ?: "deletion failed" }
                    }
                }) { Text("Delete") }
            },
            dismissButton = {
                TextButton(onClick = { pendingDelete = null }) { Text("Cancel") }
            },
        )
    }
}


/**
 * Creating a channel.
 *
 * The slug is the publish path, so it is constrained: lowercase letters,
 * digits and dashes. The field filters as one types rather than letting the
 * server refuse afterwards — the error is more useful before the request.
 */
@Composable
private fun NewChannelRow(onCreate: (String) -> Unit) {
    var slug by remember { mutableStateOf("") }

    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        OutlinedTextField(
            value = slug,
            onValueChange = { entered ->
                slug = entered.lowercase().filter { it.isLetterOrDigit() || it == '-' }
            },
            label = { Text("New channel") },
            placeholder = { Text("mediacenter") },
            singleLine = true,
            modifier = Modifier.weight(1f),
        )
        TextButton(
            enabled = slug.isNotBlank(),
            onClick = { onCreate(slug); slug = "" },
        ) { Text("Create") }
    }
}

/**
 * Shows only what needs an action.
 *
 * Nothing when everything is in order: a screen that repeats instructions
 * already followed teaches the reader to ignore it, and the warning that
 * really matters — a system update that re-enables battery optimisation —
 * would be lost in the noise.
 */
@Composable
fun ReliabilityBanner(state: ConnectionService.State) {
    // Symptom: the service claims to be running but nothing has arrived for a
    // long while, or socket failures are piling up. That is what brings back
    // the items no API can verify.
    val stale = state.running && state.lastHeartbeat > 0 &&
        System.currentTimeMillis() - state.lastHeartbeat > 5 * 60 * 1000
    val symptom = stale || state.failures >= 3

    ReliabilityChecks(LocalContext.current, symptom, title = "To do")
}

/**
 * The system settings to fix, and nothing else.
 *
 * [symptom] also brings up what no API can verify: the vendor's own
 * "sleeping apps" list, which is only recalled when something is off — or on
 * the settings screen, where one comes precisely to check.
 */
@Composable
fun ReliabilityChecks(context: Context, symptom: Boolean, title: String = "") {
    val checks = pendingChecks(context, symptom)
    if (checks.isEmpty()) return

    if (title.isNotEmpty()) {
        Spacer(Modifier.height(8.dp))
        Text(title, style = MaterialTheme.typography.titleMedium)
    }

    checks.forEach { check ->
        Card(
            modifier = Modifier.fillMaxWidth(),
            colors = CardDefaults.cardColors(
                containerColor = MaterialTheme.colorScheme.errorContainer,
            ),
        ) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Text(check.label, style = MaterialTheme.typography.titleSmall)
                Text(check.explanation, style = MaterialTheme.typography.bodySmall)
                check.fix?.let { intent ->
                    TextButton(onClick = { context.startActivity(intent()) }) {
                        Text("Open the setting")
                    }
                }
            }
        }
    }
}

/** The channels: secondary to the alerts, but this is where one creates a
 *  channel, issues a token and reads a feed. */
@Composable
private fun ChannelsScreen(settings: Settings, onOpenChannel: (ChannelPayload) -> Unit) {
    ChannelsSection(settings, onOpenChannel)
}
