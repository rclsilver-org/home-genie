package io.github.rclsilver.home_notifications.service

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
import io.github.rclsilver.home_notifications.MainActivity
import io.github.rclsilver.home_notifications.data.Settings
import io.github.rclsilver.home_notifications.net.Frame
import io.github.rclsilver.home_notifications.net.SocketClient
import io.github.rclsilver.home_notifications.net.SocketEvent
import kotlin.math.min
import kotlin.time.Duration.Companion.seconds

private const val TAG = "HomeGenie"
private const val ONGOING_CHANNEL_ID = "connection"
private const val ONGOING_NOTIFICATION_ID = 1

/**
 * Holds the connection to the server.
 *
 * A `specialUse` foreground service: the `dataSync` type is cut off after
 * 6 hours in any 24 since Android 15, which would kill the socket every
 * night. This service is what the overnight survival test measures.
 */
class ConnectionService : LifecycleService() {

    private var worker: Job? = null

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
                        update(state.value.copy(connected = true, detail = "connected"))
                    }

                    is SocketEvent.Received -> onFrame(settings, event.frame)

                    is SocketEvent.Closed -> {
                        Log.i(TAG, "socket closed: ${event.reason}")
                        update(state.value.copy(connected = false, detail = event.reason))
                    }

                    is SocketEvent.Failed -> {
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
            // injoignable.
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
            }

            Frame.HEARTBEAT -> update(
                state.value.copy(
                    lastHeartbeat = System.currentTimeMillis(),
                    heartbeats = state.value.heartbeats + 1,
                )
            )

            else -> {
                if (frame.seq > 0) settings.advanceSeq(frame.seq)
                update(
                    state.value.copy(
                        lastSeq = maxOf(state.value.lastSeq, frame.seq),
                        events = state.value.events + 1,
                        lastEvent = frame.kind,
                    )
                )
            }
        }
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

        return Notification.Builder(this, ONGOING_CHANNEL_ID)
            .setContentTitle(if (state.connected) "Connected" else "Disconnected")
            .setContentText(state.detail)
            .setSmallIcon(android.R.drawable.stat_notify_sync)
            .setContentIntent(open)
            .setOngoing(true)
            .setShowWhen(false)
            .build()
    }

    private fun notificationManager(): NotificationManager =
        getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager

    /** The observable state, read by the diagnostics screen. */
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
    ) {
        companion object {
            fun connecting() = State(running = true, connected = false, detail = "connecting…")
            fun stopped(detail: String = "stopped") =
                State(running = false, connected = false, detail = detail)
        }
    }

    companion object {
        private val state = MutableStateFlow(State.stopped())

        /** The service's current state, for the diagnostics screen. */
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
