package io.github.rclsilver.home_notifications.net

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
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
