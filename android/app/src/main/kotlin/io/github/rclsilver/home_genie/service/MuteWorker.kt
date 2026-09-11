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
import androidx.work.workDataOf
import java.time.Instant
import java.time.temporal.ChronoUnit
import io.github.rclsilver.home_genie.data.Settings
import io.github.rclsilver.home_genie.net.muteChannel

private const val TAG = "HomeGenie"

/** Sets the one-hour mute asked for from a notification. */
class MuteWorker(context: Context, params: WorkerParameters) :
    CoroutineWorker(context, params) {

    override suspend fun doWork(): Result {
        val channelId = inputData.getLong(KEY_CHANNEL_ID, 0)
        if (channelId == 0L) return Result.failure()

        val settings = Settings(applicationContext)
        val url = settings.serverUrlOnce()
        val token = settings.tokenOnce()
        if (url.isEmpty() || token.isEmpty()) return Result.failure()

        // The deadline is computed when the request is sent, not when the
        // button is pressed: a mute posted after twenty minutes of network
        // trouble would end twenty minutes early.
        val until = Instant.now().plus(1, ChronoUnit.HOURS)
        return muteChannel(url, token, channelId, until.toString()).fold(
            onSuccess = {
                Log.i(TAG, "channel $channelId muted until $until")
                Result.success()
            },
            onFailure = { error ->
                if (runAttemptCount >= MAX_ATTEMPTS) {
                    Log.w(TAG, "muting channel $channelId abandoned", error)
                    Result.failure()
                } else {
                    Result.retry()
                }
            },
        )
    }

    companion object {
        const val KEY_CHANNEL_ID = "channel_id"
        private const val MAX_ATTEMPTS = 5

        fun enqueue(context: Context, channelId: Long) {
            val request = OneTimeWorkRequestBuilder<MuteWorker>()
                .setInputData(workDataOf(KEY_CHANNEL_ID to channelId))
                .setConstraints(
                    Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build()
                )
                .build()
            // REPLACE and not KEEP: two successive requests mean "one hour from
            // now", not "ignore the second one".
            WorkManager.getInstance(context)
                .enqueueUniqueWork("mute-$channelId", ExistingWorkPolicy.REPLACE, request)
        }
    }
}
