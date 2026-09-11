package io.github.rclsilver.home_genie.service

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.util.Log
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import io.github.rclsilver.home_genie.data.Settings
import io.github.rclsilver.home_genie.net.markRead

private const val TAG = "HomeGenie"

/**
 * Swiping a notification away marks it read.
 *
 * Read, not acknowledged: an unacknowledged alert comes back at the next
 * reminder. Conflating the two would silence a critical alert with a
 * half-awake reflex.
 */
class DismissReceiver : BroadcastReceiver() {

    override fun onReceive(context: Context, intent: Intent) {
        val messageId = intent.getLongExtra(EXTRA_MESSAGE_ID, 0)
        if (messageId == 0L) return

        val pending = goAsync()
        CoroutineScope(Dispatchers.IO).launch {
            try {
                val settings = Settings(context.applicationContext)
                val url = settings.serverUrlOnce()
                val token = settings.tokenOnce()
                if (url.isEmpty() || token.isEmpty()) return@launch

                markRead(url, token, messageId).onFailure {
                    // No retry: the marking catches up at the next opening of
                    // the application. Losing a "read" is benign, unlike losing
                    // an acknowledgement.
                    Log.w(TAG, "marking $messageId as read failed", it)
                }
            } finally {
                pending.finish()
            }
        }
    }

    companion object {
        const val EXTRA_MESSAGE_ID = "message_id"
    }
}
