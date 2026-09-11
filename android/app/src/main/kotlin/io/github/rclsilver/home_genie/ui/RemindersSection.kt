package io.github.rclsilver.home_genie.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import io.github.rclsilver.home_genie.net.ReminderPolicyPayload
import io.github.rclsilver.home_genie.net.fetchReminderPolicies
import io.github.rclsilver.home_genie.net.setReminderPolicy

/** The severities one can set, even with no policy yet. */
private val SEVERITIES = listOf("critical", "warning", "info")

/**
 * Setting a channel's reminder cadences.
 *
 * Shows the **effective** policy for each severity and where it comes from:
 * without that, one would think of editing a value that does not apply, since
 * the channel override hides the default.
 */
@Composable
fun RemindersSection(channelId: Long, serverUrl: String, token: String) {
    val scope = rememberCoroutineScope()
    var policies by remember { mutableStateOf<List<ReminderPolicyPayload>>(emptyList()) }
    var error by remember { mutableStateOf("") }
    var reloads by remember { mutableStateOf(0) }

    LaunchedEffect(channelId, reloads) {
        fetchReminderPolicies(serverUrl, token, channelId)
            .onSuccess { policies = it; error = "" }
            .onFailure { error = it.message ?: "loading failed" }
    }

    Text("Reminders", style = MaterialTheme.typography.titleMedium)
    if (error.isNotEmpty()) {
        Text(error, color = MaterialTheme.colorScheme.error)
    }

    SEVERITIES.forEach { severity ->
        // The most specific wins: a channel override hides the default.
        val effective = policies.firstOrNull { it.severity == severity && it.isOverride }
            ?: policies.firstOrNull { it.severity == severity }

        ReminderCard(severity, effective) { updated ->
            scope.launch {
                setReminderPolicy(serverUrl, token, channelId, updated)
                    .onSuccess { error = ""; reloads++ }
                    .onFailure { error = it.message ?: "saving failed" }
            }
        }
    }
}

@Composable
private fun ReminderCard(
    severity: String,
    current: ReminderPolicyPayload?,
    onSave: (ReminderPolicyPayload) -> Unit,
) {
    var minutes by remember(current) {
        mutableStateOf((current?.intervalMinutes ?: 0).toString())
    }
    var enabled by remember(current) { mutableStateOf(current?.enabled ?: false) }

    Card(modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column {
                    Text(severity, style = MaterialTheme.typography.titleSmall)
                    Text(
                        when {
                            current == null -> "no policy — no reminder"
                            current.isOverride -> "override for this channel"
                            else -> "default inherited from the severity"
                        },
                        style = MaterialTheme.typography.bodySmall,
                    )
                }
                Switch(checked = enabled, onCheckedChange = { enabled = it })
            }

            OutlinedTextField(
                value = minutes,
                onValueChange = { minutes = it.filter(Char::isDigit) },
                label = { Text("Interval (minutes)") },
                singleLine = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                modifier = Modifier.fillMaxWidth(),
            )

            TextButton(onClick = {
                onSave(
                    ReminderPolicyPayload(
                        severity = severity,
                        intervalSeconds = (minutes.toIntOrNull() ?: 0) * 60,
                        enabled = enabled,
                    )
                )
            }) {
                Text("Save for this channel")
            }
        }
    }
}
