package io.github.rclsilver.home_genie.service

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import io.github.rclsilver.home_genie.data.Settings

/**
 * Restarts the service when the system stopped it for a reason that is not a
 * battery arbitration.
 *
 * After a reboot, or a night-time restart would make the survival test look
 * like a failure of the phone's skin. After an application update too:
 * Android stops the process and never starts it again, so installing a new
 * version silently cut the notifications off until someone reopened the
 * application — which nobody does, precisely because everything looks quiet.
 */
class BootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        when (intent.action) {
            Intent.ACTION_BOOT_COMPLETED, Intent.ACTION_MY_PACKAGE_REPLACED -> Unit
            else -> return
        }

        val pending = goAsync()
        CoroutineScope(Dispatchers.IO).launch {
            try {
                if (Settings(context.applicationContext).tokenOnce().isNotEmpty()) {
                    ConnectionService.start(context.applicationContext)
                }
            } finally {
                pending.finish()
            }
        }
    }
}
