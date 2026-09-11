package io.github.rclsilver.home_genie.ui

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import io.github.rclsilver.home_genie.net.PublishTokenPayload
import io.github.rclsilver.home_genie.net.createPublishToken
import io.github.rclsilver.home_genie.net.fetchPublishTokens
import io.github.rclsilver.home_genie.net.revokePublishToken

/**
 * A channel's publish tokens — what the machines carry.
 *
 * This is what makes the application self-sufficient: without this screen,
 * wiring a producer to a channel required a `curl`.
 */
@Composable
fun TokensSection(channelId: Long, channelSlug: String, serverUrl: String, token: String) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var tokens by remember { mutableStateOf<List<PublishTokenPayload>>(emptyList()) }
    var newName by remember { mutableStateOf("") }
    // The cleartext value comes back only at creation: it stays on screen
    // until the user copies it, then it is lost for good.
    var issued by remember { mutableStateOf<PublishTokenPayload?>(null) }
    var error by remember { mutableStateOf("") }
    var reloads by remember { mutableStateOf(0) }

    LaunchedEffect(channelId, reloads) {
        fetchPublishTokens(serverUrl, token, channelId)
            .onSuccess { tokens = it; error = "" }
            // A 403 is expected when one is not an owner: not a failure, just a
            // section that does not concern us.
            .onFailure { error = it.message ?: "" }
    }

    Text("Publish tokens", style = MaterialTheme.typography.titleMedium)
    Text(
        "One token per producer, write-only on this channel. Revocable without " +
            "touching the others.",
        style = MaterialTheme.typography.bodySmall,
    )
    if (error.isNotEmpty()) {
        Text(error, color = MaterialTheme.colorScheme.error)
    }

    issued?.let { fresh ->
        Card(
            modifier = Modifier.fillMaxWidth(),
            colors = CardDefaults.cardColors(
                containerColor = MaterialTheme.colorScheme.tertiaryContainer,
            ),
        ) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text("Token '${fresh.name}' — copy it now",
                    style = MaterialTheme.typography.titleSmall)
                Text(
                    "It will never be shown again: the server only keeps a " +
                        "fingerprint of it.",
                    style = MaterialTheme.typography.bodySmall,
                )
                Text(fresh.token, fontFamily = FontFamily.Monospace,
                    style = MaterialTheme.typography.bodySmall)
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    TextButton(onClick = { copy(context, fresh.token) }) { Text("Copy") }
                    TextButton(onClick = {
                        copy(context, curlExample(serverUrl, channelSlug, fresh.token))
                    }) { Text("Copy a curl example") }
                    TextButton(onClick = { issued = null }) { Text("Copied") }
                }
            }
        }
    }

    tokens.forEach { existing ->
        Card(modifier = Modifier.fillMaxWidth()) {
            Row(
                modifier = Modifier.fillMaxWidth().padding(16.dp),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column {
                    Text(existing.name, style = MaterialTheme.typography.titleSmall)
                    Text(
                        when {
                            existing.isRevoked -> "revoked"
                            existing.lastUsedAt != null ->
                                "last published ${existing.lastUsedAt}"
                            // A token never used is often a misconfigured
                            // producer: say so rather than leave a blank.
                            else -> "never used"
                        },
                        style = MaterialTheme.typography.bodySmall,
                    )
                }
                if (!existing.isRevoked) {
                    TextButton(onClick = {
                        scope.launch {
                            revokePublishToken(serverUrl, token, channelId, existing.id)
                                .onSuccess { reloads++ }
                                .onFailure { error = it.message ?: "revocation failed" }
                        }
                    }) { Text("Revoke") }
                }
            }
        }
    }

    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        OutlinedTextField(
            value = newName,
            onValueChange = { newName = it },
            label = { Text("Producer name") },
            placeholder = { Text("sonarr") },
            singleLine = true,
            modifier = Modifier.weight(1f),
        )
        TextButton(
            enabled = newName.isNotBlank(),
            onClick = {
                scope.launch {
                    createPublishToken(serverUrl, token, channelId, newName.trim())
                        .onSuccess { issued = it; newName = ""; reloads++ }
                        .onFailure { error = it.message ?: "creation failed" }
                }
            },
        ) { Text("Issue") }
    }
}

private fun copy(context: Context, text: String) {
    val manager = context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
    manager.setPrimaryClip(ClipData.newPlainText("home-genie", text))
}

/**
 * An example ready to paste into a producer's configuration. It is strict
 * ntfy syntax, which is precisely the argument for the migration.
 */
private fun curlExample(serverUrl: String, slug: String, token: String): String =
    """curl -H "Authorization: Bearer $token" """ +
        """-H "Title: My producer" -H "Priority: default" """ +
        """-d "message" ${serverUrl.trimEnd('/')}/$slug"""
