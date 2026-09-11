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
import io.github.rclsilver.home_genie.data.Settings
import io.github.rclsilver.home_genie.net.ackAlert

private const val TAG = "HomeGenie"

/**
 * Sends an acknowledgement, and retries until it lands.
 *
 * A lost acknowledgement is the worst miss available: the user made the
 * gesture, the notification disappeared, and the reminders keep firing —
 * meaning they will be woken by an alert they believe handled. The gesture
 * happens exactly where the network is least dependable, half asleep, on a
 * Wi-Fi the phone has just picked back up; the send must therefore survive
 * having no network at the moment of the tap.
 */
class AckWorker(context: Context, params: WorkerParameters) :
    CoroutineWorker(context, params) {

    override suspend fun doWork(): Result {
        val alertId = inputData.getLong(KEY_ALERT_ID, 0)
        if (alertId == 0L) return Result.failure()

        val settings = Settings(applicationContext)
        val url = settings.serverUrlOnce()
        val token = settings.tokenOnce()
        // Signed out: nobody can acknowledge anything any more, and retrying
        // forever would change nothing.
        if (url.isEmpty() || token.isEmpty()) return Result.failure()

        return ackAlert(url, token, alertId).fold(
            onSuccess = {
                Log.i(TAG, "alert $alertId acknowledged")
                Result.success()
            },
            onFailure = { error ->
                // Past that, the failure is probably no longer the network: a
                // deleted alert, a revoked token. The alert stays
                // acknowledgeable from the console.
                if (runAttemptCount >= MAX_ATTEMPTS) {
                    Log.w(TAG, "acknowledgement of $alertId given up", error)
                    Result.failure()
                } else {
                    Log.w(TAG, "acknowledgement of $alertId to be retried", error)
                    Result.retry()
                }
            },
        )
    }

    companion object {
        const val KEY_ALERT_ID = "alert_id"
        private const val MAX_ATTEMPTS = 5

        /**
         * One unique job per alert: two presses on the same button must not
         * produce two sends, and the second would have nothing more to say
         * than the first.
         */
        fun enqueue(context: Context, alertId: Long) {
            val request = OneTimeWorkRequestBuilder<AckWorker>()
                .setInputData(workDataOf(KEY_ALERT_ID to alertId))
                .setConstraints(
                    Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build()
                )
                .build()
            WorkManager.getInstance(context)
                .enqueueUniqueWork("ack-$alertId", ExistingWorkPolicy.KEEP, request)
        }
    }
}
