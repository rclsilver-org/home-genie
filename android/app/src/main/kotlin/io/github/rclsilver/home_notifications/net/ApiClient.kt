package io.github.rclsilver.home_notifications.net

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.Json
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import java.io.IOException
import java.util.concurrent.TimeUnit

private val JSON = "application/json".toMediaType()

/** HTTP client for the server. The WebSocket lives in [SocketClient]. */
class ApiClient(private val http: OkHttpClient = defaultClient()) {

    // encodeDefaults: without it kotlinx.serialization omits the fields left
    // at their default value, and the server applies its own — that is how
    // "platform" used to arrive as "unknown".
    private val json = Json {
        ignoreUnknownKeys = true
        encodeDefaults = true
    }

    suspend fun login(serverUrl: String, request: LoginRequest): Result<LoginResponse> =
        withContext(Dispatchers.IO) {
            runCatching {
                val body = json.encodeToString(LoginRequest.serializer(), request)
                    .toRequestBody(JSON)
                val call = http.newCall(
                    Request.Builder()
                        .url("${serverUrl.trimEnd('/')}/api/v1/auth/login")
                        .post(body)
                        .build()
                )
                call.execute().use { response ->
                    val text = response.body?.string().orEmpty()
                    if (!response.isSuccessful) {
                        throw IOException(errorMessage(text, response.code))
                    }
                    json.decodeFromString(LoginResponse.serializer(), text)
                }
            }
        }

    /** Pulls out the server's error message, which always has the same shape. */
    private fun errorMessage(body: String, code: Int): String =
        runCatching { json.decodeFromString(ErrorResponse.serializer(), body).error }
            .getOrElse { "HTTP error $code" }

    companion object {
        fun defaultClient(): OkHttpClient = OkHttpClient.Builder()
            .connectTimeout(15, TimeUnit.SECONDS)
            .readTimeout(30, TimeUnit.SECONDS)
            // The WebSocket carries its own application heartbeat; this one
            // keeps the TCP connection alive across NATs.
            .pingInterval(20, TimeUnit.SECONDS)
            .build()
    }
}

/** Marks a message as read. Used when a notification is swiped away. */
suspend fun markRead(serverUrl: String, token: String, messageId: Long): Result<Unit> =
    withContext(Dispatchers.IO) {
        runCatching {
            val call = ApiClient.defaultClient().newCall(
                Request.Builder()
                    .url("${serverUrl.trimEnd('/')}/api/v1/messages/$messageId/read")
                    .post(ByteArray(0).toRequestBody(null))
                    .header("Authorization", "Bearer $token")
                    .build()
            )
            call.execute().use { response ->
                if (!response.isSuccessful) throw IOException("HTTP error ${response.code}")
            }
        }
    }

/** Lists the caller's channels, with their own unread count. */
suspend fun listChannels(serverUrl: String, token: String): Result<List<ChannelPayload>> =
    withContext(Dispatchers.IO) {
        runCatching {
            val call = ApiClient.defaultClient().newCall(
                Request.Builder()
                    .url("${serverUrl.trimEnd('/')}/api/v1/channels")
                    .header("Authorization", "Bearer $token")
                    .build()
            )
            call.execute().use { response ->
                val text = response.body?.string().orEmpty()
                if (!response.isSuccessful) throw IOException("HTTP error ${response.code}")
                Json { ignoreUnknownKeys = true }
                    .decodeFromString(ListSerializer(ChannelPayload.serializer()), text)
            }
        }
    }

/** Marks a whole channel as read for the caller. */
suspend fun markChannelRead(serverUrl: String, token: String, channelId: Long): Result<Unit> =
    withContext(Dispatchers.IO) {
        runCatching {
            val call = ApiClient.defaultClient().newCall(
                Request.Builder()
                    .url("${serverUrl.trimEnd('/')}/api/v1/channels/$channelId/read")
                    .post(ByteArray(0).toRequestBody(null))
                    .header("Authorization", "Bearer $token")
                    .build()
            )
            call.execute().use { response ->
                if (!response.isSuccessful) throw IOException("HTTP error ${response.code}")
            }
        }
    }

/** A JSON body, to avoid repeating the media type. */
internal fun String.toRequestBodyJson() = this.toRequestBody(JSON)

/** Acknowledges an alert. Local to the server: nothing goes to Alertmanager. */
suspend fun ackAlert(serverUrl: String, token: String, alertId: Long): Result<Unit> =
    withContext(Dispatchers.IO) {
        runCatching {
            val call = ApiClient.defaultClient().newCall(
                Request.Builder()
                    .url("${serverUrl.trimEnd('/')}/api/v1/alerts/$alertId/ack")
                    .post(ByteArray(0).toRequestBody(null))
                    .header("Authorization", "Bearer $token")
                    .build()
            )
            call.execute().use { response ->
                if (!response.isSuccessful) throw IOException("HTTP error ${response.code}")
            }
        }
    }

/** A channel's feed, newest first. */
suspend fun fetchMessages(serverUrl: String, token: String, channelId: Long, limit: Int = 50):
    Result<List<MessagePayload>> = withContext(Dispatchers.IO) {
    runCatching {
        get(serverUrl, token, "/api/v1/channels/$channelId/messages?limit=$limit") { text ->
            Json { ignoreUnknownKeys = true }
                .decodeFromString(ListSerializer(MessagePayload.serializer()), text)
        }
    }
}

/** A channel's alerts; openOnly restricts to those still open. */
suspend fun fetchAlerts(serverUrl: String, token: String, channelId: Long, openOnly: Boolean):
    Result<List<AlertPayload>> = withContext(Dispatchers.IO) {
    runCatching {
        val suffix = if (openOnly) "?open=1" else ""
        get(serverUrl, token, "/api/v1/channels/$channelId/alerts$suffix") { text ->
            Json { ignoreUnknownKeys = true }
                .decodeFromString(ListSerializer(AlertPayload.serializer()), text)
        }
    }
}

/** Marks a channel read up to the given message, or entirely. */
suspend fun markChannelReadUpTo(serverUrl: String, token: String, channelId: Long, uptoId: Long):
    Result<Unit> = withContext(Dispatchers.IO) {
    runCatching {
        val suffix = if (uptoId > 0) "?upto_id=$uptoId" else ""
        val call = ApiClient.defaultClient().newCall(
            Request.Builder()
                .url("${serverUrl.trimEnd('/')}/api/v1/channels/$channelId/read$suffix")
                .post(ByteArray(0).toRequestBody(null))
                .header("Authorization", "Bearer $token")
                .build()
        )
        call.execute().use { response ->
            if (!response.isSuccessful) throw IOException("HTTP error ${response.code}")
        }
    }
}

/** An authenticated GET, so the plumbing is not repeated on every call. */
private fun <T> get(serverUrl: String, token: String, path: String, decode: (String) -> T): T {
    val call = ApiClient.defaultClient().newCall(
        Request.Builder()
            .url("${serverUrl.trimEnd('/')}$path")
            .header("Authorization", "Bearer $token")
            .build()
    )
    return call.execute().use { response ->
        val text = response.body?.string().orEmpty()
        if (!response.isSuccessful) throw IOException("HTTP error ${response.code}")
        decode(text)
    }
}

/** The cadences applying to a channel: its overrides and inherited defaults. */
suspend fun fetchReminderPolicies(serverUrl: String, token: String, channelId: Long):
    Result<List<ReminderPolicyPayload>> = withContext(Dispatchers.IO) {
    runCatching {
        get(serverUrl, token, "/api/v1/channels/$channelId/reminders") { text ->
            Json { ignoreUnknownKeys = true }
                .decodeFromString(ListSerializer(ReminderPolicyPayload.serializer()), text)
        }
    }
}

/** Sets the channel's override for a severity. Owner only. */
suspend fun setReminderPolicy(
    serverUrl: String,
    token: String,
    channelId: Long,
    policy: ReminderPolicyPayload,
): Result<Unit> = withContext(Dispatchers.IO) {
    runCatching {
        val payload = Json.encodeToString(ReminderPolicyPayload.serializer(), policy)
        val call = ApiClient.defaultClient().newCall(
            Request.Builder()
                .url("${serverUrl.trimEnd('/')}/api/v1/channels/$channelId/reminders")
                .put(payload.toRequestBodyJson())
                .header("Authorization", "Bearer $token")
                .build()
        )
        call.execute().use { response ->
            val text = response.body?.string().orEmpty()
            if (!response.isSuccessful) {
                val message = runCatching {
                    Json { ignoreUnknownKeys = true }
                        .decodeFromString(ErrorResponse.serializer(), text).error
                }.getOrElse { "HTTP error ${response.code}" }
                throw IOException(message)
            }
        }
    }
}

/** Mutes a channel until an instant, or lifts the mute. */
suspend fun muteChannel(serverUrl: String, token: String, channelId: Long, untilRfc3339: String):
    Result<Unit> = withContext(Dispatchers.IO) {
    runCatching {
        val body = """{"muted_until":"$untilRfc3339"}"""
        val call = ApiClient.defaultClient().newCall(
            Request.Builder()
                .url("${serverUrl.trimEnd('/')}/api/v1/channels/$channelId")
                .patch(body.toRequestBodyJson())
                .header("Authorization", "Bearer $token")
                .build()
        )
        call.execute().use { response ->
            if (!response.isSuccessful) throw IOException("HTTP error ${response.code}")
        }
    }
}

/** Creates a channel; the caller becomes its owner. */
suspend fun createChannel(serverUrl: String, token: String, request: CreateChannelRequest):
    Result<ChannelPayload> = withContext(Dispatchers.IO) {
    runCatching {
        postJson(
            serverUrl, token, "/api/v1/channels",
            Json.encodeToString(CreateChannelRequest.serializer(), request),
        ) { text ->
            Json { ignoreUnknownKeys = true }
                .decodeFromString(ChannelPayload.serializer(), text)
        }
    }
}

/** A channel's publish tokens, revoked ones included. */
suspend fun fetchPublishTokens(serverUrl: String, token: String, channelId: Long):
    Result<List<PublishTokenPayload>> = withContext(Dispatchers.IO) {
    runCatching {
        get(serverUrl, token, "/api/v1/channels/$channelId/tokens") { text ->
            Json { ignoreUnknownKeys = true }
                .decodeFromString(ListSerializer(PublishTokenPayload.serializer()), text)
        }
    }
}

/** Issues a publish token. The cleartext value comes back only here. */
suspend fun createPublishToken(serverUrl: String, token: String, channelId: Long, name: String):
    Result<PublishTokenPayload> = withContext(Dispatchers.IO) {
    runCatching {
        postJson(
            serverUrl, token, "/api/v1/channels/$channelId/tokens",
            Json.encodeToString(CreateTokenRequest.serializer(), CreateTokenRequest(name)),
        ) { text ->
            Json { ignoreUnknownKeys = true }
                .decodeFromString(PublishTokenPayload.serializer(), text)
        }
    }
}

/** Revokes a token. Immediate: it stops resolving at once. */
suspend fun revokePublishToken(serverUrl: String, token: String, channelId: Long, tokenId: Long):
    Result<Unit> = withContext(Dispatchers.IO) {
    runCatching {
        val call = ApiClient.defaultClient().newCall(
            Request.Builder()
                .url("${serverUrl.trimEnd('/')}/api/v1/channels/$channelId/tokens/$tokenId")
                .delete()
                .header("Authorization", "Bearer $token")
                .build()
        )
        call.execute().use { response ->
            if (!response.isSuccessful) throw IOException("HTTP error ${response.code}")
        }
    }
}

/** An authenticated JSON POST whose response is read. */
private fun <T> postJson(
    serverUrl: String,
    token: String,
    path: String,
    payload: String,
    decode: (String) -> T,
): T {
    val call = ApiClient.defaultClient().newCall(
        Request.Builder()
            .url("${serverUrl.trimEnd('/')}$path")
            .post(payload.toRequestBodyJson())
            .header("Authorization", "Bearer $token")
            .build()
    )
    return call.execute().use { response ->
        val text = response.body?.string().orEmpty()
        if (!response.isSuccessful) {
            val message = runCatching {
                Json { ignoreUnknownKeys = true }
                    .decodeFromString(ErrorResponse.serializer(), text).error
            }.getOrElse { "HTTP error ${response.code}" }
            throw IOException(message)
        }
        decode(text)
    }
}
