package io.github.rclsilver.home_notifications.ui

import android.app.NotificationManager
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.PowerManager
import androidx.core.content.ContextCompat
import android.provider.Settings as AndroidSettings

/**
 * The system settings the service's survival depends on.
 *
 * Nothing shows while everything is in order. A screen repeating advice
 * already followed teaches the reader to ignore it, and the day a system
 * update quietly re-enables battery optimisation, the warning would be lost
 * in its own noise.
 */
data class ReliabilityCheck(
    val label: String,
    val explanation: String,
    val fix: (() -> Intent)?,
)

/**
 * What needs an action, and nothing else.
 *
 * [symptom] brings back the items that cannot be verified: a manufacturer's
 * "sleeping apps" list is exposed by no API, so it is only raised when a
 * symptom suggests it is biting.
 */
fun pendingChecks(context: Context, symptom: Boolean): List<ReliabilityCheck> {
    val checks = mutableListOf<ReliabilityCheck>()

    val power = context.getSystemService(Context.POWER_SERVICE) as PowerManager
    if (!power.isIgnoringBatteryOptimizations(context.packageName)) {
        checks += ReliabilityCheck(
            label = "Battery optimisation to disable",
            explanation = "Without an exemption the system suspends the service " +
                "during sleep, and the alerts stop arriving.",
            fix = {
                Intent(
                    AndroidSettings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS,
                    Uri.parse("package:${context.packageName}"),
                )
            },
        )
    }

    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
        ContextCompat.checkSelfPermission(context, android.Manifest.permission.POST_NOTIFICATIONS)
        != PackageManager.PERMISSION_GRANTED
    ) {
        checks += ReliabilityCheck(
            label = "Notifications to allow",
            explanation = "Without this permission no alert can be shown, and " +
                "Android refuses the foreground service.",
            fix = {
                Intent(AndroidSettings.ACTION_APP_NOTIFICATION_SETTINGS)
                    .putExtra(AndroidSettings.EXTRA_APP_PACKAGE, context.packageName)
            },
        )
    }

    val notifications =
        context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
    if (!notifications.isNotificationPolicyAccessGranted) {
        checks += ReliabilityCheck(
            label = "Do Not Disturb access",
            explanation = "Without this access a critical alert stays silent while " +
                "the phone is in Do Not Disturb: Android ignores the flag without " +
                "saying a word.",
            fix = { Intent(AndroidSettings.ACTION_NOTIFICATION_POLICY_ACCESS_SETTINGS) },
        )
    }

    // Unverifiable by program: the list is exposed by no API. It is therefore
    // only raised when something is actually wrong, rather than showing
    // permanently an instruction one cannot know has been followed.
    if (symptom) {
        checks += ReliabilityCheck(
            label = "Check the \"sleeping apps\" list",
            explanation = "The connection is unstable. Under Battery → Background " +
                "usage limits, the application must be neither sleeping nor deep " +
                "sleeping.",
            fix = {
                Intent(AndroidSettings.ACTION_APPLICATION_DETAILS_SETTINGS)
                    .setData(Uri.parse("package:${context.packageName}"))
            },
        )
    }

    return checks
}
