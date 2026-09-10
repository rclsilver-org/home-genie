package io.github.rclsilver.home_notifications.data

import android.content.Context
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.longPreferencesKey
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map

private val Context.dataStore by preferencesDataStore(name = "settings")

/**
 * What the device has to remember between two launches.
 *
 * [lastSeq] is the resynchronisation cursor: it is what makes a socket killed
 * by the system lose nothing, since the reconnection starts from there. It is
 * persisted rather than kept in memory.
 */
class Settings(private val context: Context) {

    private object Keys {
        val serverUrl = stringPreferencesKey("server_url")
        val token = stringPreferencesKey("token")
        val username = stringPreferencesKey("username")
        val lastSeq = longPreferencesKey("last_seq")
    }

    val serverUrl: Flow<String> = context.dataStore.data.map { it[Keys.serverUrl] ?: "" }
    val token: Flow<String> = context.dataStore.data.map { it[Keys.token] ?: "" }
    val username: Flow<String> = context.dataStore.data.map { it[Keys.username] ?: "" }
    val lastSeq: Flow<Long> = context.dataStore.data.map { it[Keys.lastSeq] ?: 0L }

    suspend fun serverUrlOnce(): String = serverUrl.first()
    suspend fun tokenOnce(): String = token.first()
    suspend fun lastSeqOnce(): Long = lastSeq.first()

    suspend fun saveSession(serverUrl: String, token: String, username: String) {
        context.dataStore.edit {
            it[Keys.serverUrl] = serverUrl
            it[Keys.token] = token
            it[Keys.username] = username
        }
    }

    /** Advances the cursor. Never rewinds: a replay must not wind it back. */
    suspend fun advanceSeq(seq: Long) {
        context.dataStore.edit {
            val current = it[Keys.lastSeq] ?: 0L
            if (seq > current) it[Keys.lastSeq] = seq
        }
    }

    /** Clears the session; the cursor goes with it, it is worth nothing now. */
    suspend fun clear() {
        context.dataStore.edit {
            it.remove(Keys.token)
            it.remove(Keys.username)
            it.remove(Keys.lastSeq)
        }
    }
}

/**
 * Transient state of the OIDC flow.
 *
 * Persisted rather than kept in memory: the Custom Tab sends the
 * application to the background, and Android may kill the process while
 * the user authenticates. A lost verifier would make the code unusable.
 */
class PendingLogin(private val context: Context) {

    private object Keys {
        val verifier = stringPreferencesKey("pkce_verifier")
        val state = stringPreferencesKey("oidc_state")
        val issuer = stringPreferencesKey("oidc_issuer")
        val clientId = stringPreferencesKey("oidc_client_id")
        val serverUrl = stringPreferencesKey("oidc_server_url")
    }

    data class Pending(
        val verifier: String,
        val state: String,
        val issuer: String,
        val clientId: String,
        val serverUrl: String,
    ) {
        val isPresent: Boolean get() = verifier.isNotEmpty() && state.isNotEmpty()
    }

    suspend fun save(verifier: String, state: String, issuer: String, clientId: String, serverUrl: String) {
        context.dataStore.edit {
            it[Keys.verifier] = verifier
            it[Keys.state] = state
            it[Keys.issuer] = issuer
            it[Keys.clientId] = clientId
            it[Keys.serverUrl] = serverUrl
        }
    }

    suspend fun read(): Pending {
        val prefs = context.dataStore.data.first()
        return Pending(
            verifier = prefs[Keys.verifier] ?: "",
            state = prefs[Keys.state] ?: "",
            issuer = prefs[Keys.issuer] ?: "",
            clientId = prefs[Keys.clientId] ?: "",
            serverUrl = prefs[Keys.serverUrl] ?: "",
        )
    }

    /** Erased as soon as the exchange happens: a verifier serves once. */
    suspend fun clear() {
        context.dataStore.edit {
            Keys.let { k ->
                it.remove(k.verifier); it.remove(k.state)
                it.remove(k.issuer); it.remove(k.clientId); it.remove(k.serverUrl)
            }
        }
    }
}
