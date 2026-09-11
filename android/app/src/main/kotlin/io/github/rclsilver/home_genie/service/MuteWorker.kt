package io.github.rclsilver.home_genie.service

import android.content.Context
import android.util.Log
import androidx.work.Constraints
import androidx.work.CoroutineWorker
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import io.github.rclsilver.home_genie.data.Settings
import io.github.rclsilver.home_genie.net.setMute
import java.time.Instant
import java.time.temporal.ChronoUnit

private const val TAG = "HomeGenie"

/**
 * Sets my own mute for an hour, asked for from a notification.
 *
 * It concerns the person holding the phone alone: the server files it on
 * their account, not on the channel nor on the installation.
 */
class MuteWorker(context: Context, params: WorkerParameters) :
    CoroutineWorker(context, params) {

    override suspend fun doWork(): Result {
        val settings = Settings(applicationContext)
        val url = settings.serverUrlOnce()
        val token = settings.tokenOnce()
        if (url.isEmpty() || token.isEmpty()) return Result.failure()

        // The deadline is computed when the request is sent, not when the
        // button is pressed: a mute posted after twenty minutes of network
        // trouble would end twenty minutes early.
        val until = Instant.now().plus(1, ChronoUnit.HOURS)
        return setMute(url, token, until.toString()).fold(
            onSuccess = {
                Log.i(TAG, "muted until $until")
                Result.success()
            },
            onFailure = { error ->
                if (runAttemptCount >= MAX_ATTEMPTS) {
                    Log.w(TAG, "mute given up", error)
                    Result.failure()
                } else {
                    Result.retry()
                }
            },
        )
    }

    companion object {
        private const val MAX_ATTEMPTS = 5

        fun enqueue(context: Context) {
            val request = OneTimeWorkRequestBuilder<MuteWorker>()
                .setConstraints(
                    Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build()
                )
                .build()
            // REPLACE and not KEEP: two successive requests mean "one hour from
            // now", not "ignore the second one".
            WorkManager.getInstance(context)
                .enqueueUniqueWork("mute", ExistingWorkPolicy.REPLACE, request)
        }
    }
}
