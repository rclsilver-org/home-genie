package io.github.rclsilver.home_genie.net

import android.util.Log
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.serialization.json.Json
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener

private const val TAG = "HomeGenie"

/** What the socket reports back to the service. */
sealed interface SocketEvent {
    data object Connecting : SocketEvent

    /**
     * The socket is open. [send] emits — that is how the acknowledgement
     * leaves, over the same connection as what it acknowledges, so that a
     * dead socket makes the acknowledgement impossible rather than falsely
     * successful.
     */
    data class Open(val send: (String) -> Boolean) : SocketEvent
    data class Received(val frame: Frame) : SocketEvent
    data class Closed(val reason: String) : SocketEvent
    data class Failed(val error: Throwable) : SocketEvent
}

/**
 * A connection to the live stream.
 *
 * The class implements **no** reconnection policy: it exposes a flow that
 * ends when the socket dies, and the service decides whether to reopen.
 * Keeping that decision in one place avoids two reconnection loops fighting
 * each other.
 */
class SocketClient(private val http: OkHttpClient = ApiClient.shared) {

    private val json = Json { ignoreUnknownKeys = true }

    fun connect(serverUrl: String, token: String, sinceSeq: Long): Flow<SocketEvent> = callbackFlow {
        val url = buildString {
            append(serverUrl.trimEnd('/').replaceFirst("http", "ws"))
            append("/api/v1/ws")
            if (sinceSeq > 0) append("?since_seq=").append(sinceSeq)
        }

        trySend(SocketEvent.Connecting)

        val request = Request.Builder()
            .url(url)
            .header("Authorization", "Bearer $token")
            .build()

        val socket = http.newWebSocket(request, object : WebSocketListener() {
            override fun onOpen(webSocket: WebSocket, response: Response) {
                trySend(SocketEvent.Open(webSocket::send))
            }

            override fun onMessage(webSocket: WebSocket, text: String) {
                val frame = runCatching { json.decodeFromString(Frame.serializer(), text) }
                    .getOrElse {
                        // An unreadable frame must not kill the connection:
                        // it is probably a server newer than the app.
                        Log.w(TAG, "unreadable frame: $text", it)
                        return
                    }
                trySend(SocketEvent.Received(frame))
            }

            override fun onClosing(webSocket: WebSocket, code: Int, reason: String) {
                webSocket.close(1000, null)
                trySend(SocketEvent.Closed(reason.ifEmpty { "code $code" }))
                close()
            }

            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                trySend(SocketEvent.Failed(t))
                close()
            }
        })

        awaitClose { socket.cancel() }
    }
}
