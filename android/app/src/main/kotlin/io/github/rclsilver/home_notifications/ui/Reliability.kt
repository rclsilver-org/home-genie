package io.github.rclsilver.home_notifications.ui

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.PowerManager
import android.provider.Settings as AndroidSettings

/**
 * The system settings the service's survival depends on.
 *
 * This is not a reminder list: a vendor skin sometimes re-enables battery
 * optimisation after a system update, and a service killed with nothing to
 * signal it looks exactly like a server outage. The screen therefore checks
 * them every time it is shown.
 */
data class ReliabilityCheck(
    val label: String,
    val satisfied: Boolean,
    val explanation: String,
    val fix: (() -> Intent)?,
)

fun reliabilityChecks(context: Context): List<ReliabilityCheck> {
    val power = context.getSystemService(Context.POWER_SERVICE) as PowerManager
    val exempt = power.isIgnoringBatteryOptimizations(context.packageName)

    return listOf(
        ReliabilityCheck(
            label = "Battery optimisation disabled",
            satisfied = exempt,
            explanation = "Without an exemption, the system suspends the service during " +
                "sleep and the alerts stop arriving. A system update sometimes " +
                "after an update.",
            fix = if (exempt) null else {
                {
                    Intent(
                        AndroidSettings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS,
                        Uri.parse("package:${context.packageName}"),
                    )
                }
            },
        ),
        ReliabilityCheck(
            label = "Sleeping apps (vendor list)",
            // Not verifiable programmatically: the list is exposed by no API.
            // It is shown as an action to take, not as a state, rather than
            // pretending to know.
            satisfied = false,
            explanation = "Check that the application is in neither 'Sleeping " +
                "apps' nor in 'Deep sleeping apps', under " +
                "Battery → Background usage limits.",
            fix = { Intent(AndroidSettings.ACTION_APPLICATION_DETAILS_SETTINGS)
                .setData(Uri.parse("package:${context.packageName}")) },
        ),
    )
}
