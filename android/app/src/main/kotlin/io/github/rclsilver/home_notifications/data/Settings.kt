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
