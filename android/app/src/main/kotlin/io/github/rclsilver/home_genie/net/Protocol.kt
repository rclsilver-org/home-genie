package io.github.rclsilver.home_genie.net

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement

/** A mirror of the contract described in docs/protocol.md. */

@Serializable
data class LoginRequest(
    val username: String,
    val password: String,
    @SerialName("device_name") val deviceName: String,
    val platform: String = "android",
)

@Serializable
data class LoginResponse(
    val token: String,
    val user: User,
    val device: Device,
)

@Serializable
data class User(
    val id: Long,
    val username: String,
    @SerialName("display_name") val displayName: String = "",
    @SerialName("is_admin") val isAdmin: Boolean = false,
    @SerialName("is_local") val isLocal: Boolean = false,
)

@Serializable
data class Device(
    val id: Long,
    val name: String,
    val platform: String = "",
    val transport: String = "",
)

@Serializable
data class ErrorResponse(val error: String)

/**
 * A frame of the live stream.
 *
 * [seq] is absent from transport frames (`ready`, `heartbeat`) and present
 * on persisted events — it is the one advanced in the settings.
 */
@Serializable
data class Frame(
    val kind: String,
    val seq: Long = 0,
    val at: String = "",
    val payload: JsonElement? = null,
) {
    companion object {
        const val READY = "ready"
        const val HEARTBEAT = "heartbeat"
    }
}
