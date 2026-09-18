package io.github.rclsilver.home_genie.service

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.os.Build
import android.util.Log
import androidx.lifecycle.LifecycleService
import androidx.lifecycle.lifecycleScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.serialization.json.Json
import io.github.rclsilver.home_genie.MainActivity
import io.github.rclsilver.home_genie.data.Settings
import io.github.rclsilver.home_genie.net.Frame
import io.github.rclsilver.home_genie.net.MessagePayload
import io.github.rclsilver.home_genie.net.fetchAllAlerts
import io.github.rclsilver.home_genie.net.MessagesReadPayload
import io.github.rclsilver.home_genie.net.SocketClient
import io.github.rclsilver.home_genie.net.SocketEvent
import kotlin.math.min
import kotlin.time.Duration.Companion.seconds

private const val TAG = "HomeGenie"
private const val ONGOING_CHANNEL_ID = "connection"
private const val ONGOING_NOTIFICATION_ID = 1
private const val EVENT_MESSAGE_NEW = "message.new"
private const val EVENT_MESSAGES_READ = "messages.read"

/**
 * Holds the connection to the server.
 *
 * A `specialUse` foreground service: the `dataSync` type is cut off after
 * 6 hours in any 24 since Android 15, which would kill the socket every
 * night. This service is what the overnight survival test measures.
 */
class ConnectionService : LifecycleService() {

    private var worker: Job? = null
    private var send: ((String) -> Boolean)? = null
    private val notifier by lazy { Notifier(applicationContext) }
    private val json = Json { ignoreUnknownKeys = true }

    override fun onCreate() {
        super.onCreate()
        createNotificationChannel()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        super.onStartCommand(intent, flags, startId)
        startForeground(ONGOING_NOTIFICATION_ID, buildNotification(State.connecting()))

        if (worker == null) {
            worker = lifecycleScope.launch { run() }
        }

        // START_STICKY: if the system kills us for memory, it starts us
        // again. It will not if the user force-stops the application, nor on
        // some manufacturers — which is precisely what the test measures.
        return START_STICKY
    }

    override fun onDestroy() {
        Log.i(TAG, "service destroyed")
        worker?.cancel()
        worker = null
        send = null
        state.value = State.stopped()
        super.onDestroy()
    }

    /**
     * The connection loop. The backoff is exponential and capped: on an
     * absent network, retrying every second forever would drain the
     * battery for nothing.
     */
    private suspend fun run() {
        val settings = Settings(applicationContext)
        val client = SocketClient()
        var backoffSeconds = 1L

        while (true) {
            val serverUrl = settings.serverUrlOnce()
            val token = settings.tokenOnce()
            if (serverUrl.isEmpty() || token.isEmpty()) {
                update(State.stopped("no session"))
                delay(30.seconds)
                continue
            }

            val sinceSeq = settings.lastSeqOnce()
            var opened = false

            client.connect(serverUrl, token, sinceSeq).collect { event ->
                when (event) {
                    is SocketEvent.Connecting -> update(State.connecting())

                    is SocketEvent.Open -> {
                        opened = true
                        backoffSeconds = 1
                        send = event.send
                        update(state.value.copy(connected = true, detail = "connected"))
                    }

                    is SocketEvent.Received -> onFrame(settings, event.frame)

                    is SocketEvent.Closed -> {
                        send = null
                        Log.i(TAG, "socket closed: ${event.reason}")
                        update(state.value.copy(connected = false, detail = event.reason))
                    }

                    is SocketEvent.Failed -> {
                        send = null
                        Log.w(TAG, "socket failed", event.error)
                        update(
                            state.value.copy(
                                connected = false,
                                detail = event.error.message ?: "failed",
                                failures = state.value.failures + 1,
                            )
                        )
                    }
                }
            }

            // A socket that had opened comes back at once; one that never
            // made it waits, so as not to hammer a server that may be down.
            if (!opened) {
                delay(backoffSeconds.seconds)
                backoffSeconds = min(backoffSeconds * 2, 60)
            }
        }
    }

    private suspend fun onFrame(settings: Settings, frame: Frame) {
        when (frame.kind) {
            Frame.READY -> {
                if (frame.seq > 0) settings.advanceSeq(frame.seq)
                update(
                    state.value.copy(
                        connected = true,
                        detail = "up to date",
                        lastSeq = maxOf(state.value.lastSeq, frame.seq),
                    )
                )
                refreshOpenAlerts(settings)
                return
            }

            Frame.HEARTBEAT -> {
                update(
                    state.value.copy(
                        lastHeartbeat = System.currentTimeMillis(),
                        heartbeats = state.value.heartbeats + 1,
                    )
                )
                // Answering is the only thing that proves to the server that
                // the socket is alive in this direction: writing into a frozen
                // socket succeeds for a long time while nothing arrives.
                if (send?.invoke("""{"kind":"pong","seq":0}""") != true) {
                    Log.w(TAG, "pong not sent")
                }
                return
            }

            EVENT_MESSAGE_NEW -> {
                frame.payload?.let { raw ->
                    runCatching { json.decodeFromJsonElement(MessagePayload.serializer(), raw) }
                        // `silent` is the mute: the message arrives, counts as
                        // unread, and does not notify. That is what tells it
                        // from the quiet hours, which merely
                        // lower the priority.
                        .onSuccess { if (!it.read && !it.silent) notifier.show(it) }
                        .onFailure { Log.w(TAG, "unreadable message", it) }
                }
            }

            EVENT_MESSAGES_READ -> {
                // Read on another of my devices: the notification disappears
                // here too, without concerning the other members.
                frame.payload?.let { raw ->
                    runCatching { json.decodeFromJsonElement(MessagesReadPayload.serializer(), raw) }
                        .onSuccess { payload -> notifier.cancelAll(payload.messageIds) }
                        .onFailure { Log.w(TAG, "unreadable read event", it) }
                }
            }
        }

        // Every lifecycle event moves the count, and the ongoing line is read
        // without opening anything — so it is refreshed from the server rather
        // than incremented here.
        if (frame.kind.startsWith("alert.")) {
            refreshOpenAlerts(settings)
        }

        recordEvent(settings, frame)
        acknowledge(frame)
    }

    /**
     * Re-reads what is still open. Failure leaves the previous count in place:
     * a transient error must not make the line claim the house is quiet.
     */
    private suspend fun refreshOpenAlerts(settings: Settings) {
        val serverUrl = settings.serverUrlOnce()
        val token = settings.tokenOnce()
        if (serverUrl.isEmpty() || token.isEmpty()) return

        fetchAllAlerts(serverUrl, token, openOnly = true).onSuccess { alerts ->
            update(
                state.value.copy(
                    open = OpenAlerts(
                        critical = alerts.count { it.severity == "critical" },
                        warning = alerts.count { it.severity == "warning" },
                        info = alerts.count { it.severity == "info" },
                        other = alerts.count {
                            it.severity !in setOf("critical", "warning", "info")
                        },
                    )
                )
            )
        }
    }

    private suspend fun recordEvent(settings: Settings, frame: Frame) {
        if (frame.seq > 0) settings.advanceSeq(frame.seq)
        update(
            state.value.copy(
                lastSeq = maxOf(state.value.lastSeq, frame.seq),
                events = state.value.events + 1,
                lastEvent = frame.kind,
            )
        )
    }

    /**
     * The acknowledgement goes out on the socket that delivered, never over a
     * separate HTTP request: a dead socket makes the acknowledgement
     * impossible rather than falsely successful. On the server, "sent with no
     * acknowledgement" is exactly the signature of a socket the system froze
     * without closing.
     */
    private fun acknowledge(frame: Frame) {
        if (frame.seq <= 0) return
        val emitted = send?.invoke("""{"kind":"ack","seq":${frame.seq}}""") ?: false
        if (!emitted) Log.w(TAG, "acknowledgement not sent for seq=${frame.seq}")
    }

    private fun update(next: State) {
        state.value = next
        notificationManager().notify(ONGOING_NOTIFICATION_ID, buildNotification(next))
    }

    private fun createNotificationChannel() {
        val channel = NotificationChannel(
            ONGOING_CHANNEL_ID,
            "Connection",
            // IMPORTANCE_LOW: the ongoing notification is mandatory for a
            // foreground service, but it must not make noise.
            NotificationManager.IMPORTANCE_LOW,
        ).apply {
            description = "Ongoing notification of the service holding the connection"
            setShowBadge(false)
        }
        notificationManager().createNotificationChannel(channel)
    }

    private fun buildNotification(state: State): Notification {
        val open = PendingIntent.getActivity(
            this, 0,
            Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_IMMUTABLE,
        )

        // What the line says depends on what is worth knowing. Connected, the
        // question is what is waiting; disconnected, the connection is the
        // answer — and it is the one case worth saying out loud, since with no
        // socket nothing arrives and the silence looks like calm.
        val title = when {
            !state.running -> "Service stopped"
            !state.connected -> "Disconnected — reconnecting"
            state.open.total > 0 -> state.open.summary()
            else -> "Nothing to handle"
        }

        return Notification.Builder(this, ONGOING_CHANNEL_ID)
            .setContentTitle(title)
            .setContentText(if (state.connected) "Connected" else state.detail)
            .setSmallIcon(android.R.drawable.stat_notify_sync)
            .setContentIntent(open)
            .setOngoing(true)
            .setShowWhen(false)
            .build()
    }

    private fun notificationManager(): NotificationManager =
        getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager

    /** The observable state the screens read. */
    data class State(
        val running: Boolean,
        val connected: Boolean,
        val detail: String,
        val lastSeq: Long = 0,
        val lastHeartbeat: Long = 0,
        val heartbeats: Long = 0,
        val events: Long = 0,
        val failures: Long = 0,
        val lastEvent: String = "",
        val startedAt: Long = System.currentTimeMillis(),
        val open: OpenAlerts = OpenAlerts(),
    ) {
        companion object {
            fun connecting() = State(running = true, connected = false, detail = "connecting…")
            fun stopped(detail: String = "stopped") =
                State(running = false, connected = false, detail = detail)
        }
    }

    /**
     * What is still open, by severity — the one thing worth reading in the
     * ongoing notification.
     *
     * Counted by the server and not tracked here: a count kept in parallel
     * drifts, and this one is glanced at precisely when it must not be wrong.
     */
    data class OpenAlerts(
        val critical: Int = 0,
        val warning: Int = 0,
        val info: Int = 0,
        // Whatever a rule sends that is none of the three. Kept separate
        // rather than folded into info: a severity nobody planned for should
        // look unplanned, not quietly reclassified.
        val other: Int = 0,
    ) {
        val total: Int get() = critical + warning + info + other

        /**
         * Reads as a sentence at a glance: "2 critical, 1 warning". Severities
         * that count zero are left out — a line full of zeros is one the eye
         * stops parsing, and the whole point is to be read without effort.
         */
        fun summary(): String = listOfNotNull(
            critical.takeIf { it > 0 }?.let { "$it critical" },
            warning.takeIf { it > 0 }?.let { "$it warning" },
            info.takeIf { it > 0 }?.let { "$it info" },
            other.takeIf { it > 0 }?.let { "$it other" },
        ).joinToString(", ")
    }

    companion object {
        private val state = MutableStateFlow(State.stopped())

        /** The service's current state, for whatever displays it. */
        val observedState: StateFlow<State> = state.asStateFlow()

        fun start(context: Context) {
            val intent = Intent(context, ConnectionService::class.java)
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                context.startForegroundService(intent)
            } else {
                context.startService(intent)
            }
        }

        fun stop(context: Context) {
            context.stopService(Intent(context, ConnectionService::class.java))
        }
    }
}
