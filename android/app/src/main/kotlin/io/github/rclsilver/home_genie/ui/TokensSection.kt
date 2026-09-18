package io.github.rclsilver.home_genie.ui

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.material3.AlertDialog
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material3.Card
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import io.github.rclsilver.home_genie.net.PublishTokenPayload
import io.github.rclsilver.home_genie.net.createPublishToken
import io.github.rclsilver.home_genie.net.fetchPublishTokens
import io.github.rclsilver.home_genie.net.revokePublishToken
import io.github.rclsilver.home_genie.net.setProducerIcon

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
    // The cleartext value comes back only at creation: it stays on screen
    // until the user copies it, then it is lost for good.
    var issued by remember { mutableStateOf<PublishTokenPayload?>(null) }
    var error by remember { mutableStateOf("") }
    var reloads by remember { mutableStateOf(0) }

    // Which producer a picked image is for. The picker is a single launcher
    // rather than one per row — it returns to whoever opened it, and a launcher
    // created inside a list would be rebuilt every time the list reloads.
    var pickingFor by remember { mutableStateOf<Long?>(null) }
    val picker = rememberLauncherForActivityResult(ActivityResultContracts.GetContent()) { uri ->
        val target = pickingFor
        pickingFor = null
        if (uri == null || target == null) return@rememberLauncherForActivityResult
        scope.launch {
            val bytes = withContext(Dispatchers.IO) {
                runCatching { context.contentResolver.openInputStream(uri)?.use { it.readBytes() } }
                    .getOrNull()
            }
            when {
                bytes == null -> error = "that image could not be read"
                // Checked here as well as on the server, only to spare a
                // pointless upload: a photograph straight off a camera is
                // several megabytes and would be refused on arrival.
                bytes.size > MAX_ICON_BYTES ->
                    error = "an icon must be under ${MAX_ICON_BYTES / 1024} kB"
                else -> setProducerIcon(serverUrl, token, channelId, target, bytes)
                    .onSuccess { IconCache.forget(target); reloads++; error = "" }
                    .onFailure { error = it.message ?: "the upload failed" }
            }
        }
    }

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
            "touching the others, and each can wear its own picture.",
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
        key(existing.id) {
            TokenRow(
                producer = existing,
                serverUrl = serverUrl,
                token = token,
                onPickIcon = { pickingFor = existing.id; picker.launch("image/*") },
                onRemoveIcon = {
                    scope.launch {
                        setProducerIcon(serverUrl, token, channelId, existing.id, ByteArray(0))
                            .onSuccess { IconCache.forget(existing.id); reloads++; error = "" }
                            .onFailure { error = it.message ?: "failed" }
                    }
                },
                onRevoke = {
                    scope.launch {
                        revokePublishToken(serverUrl, token, channelId, existing.id)
                            .onSuccess { reloads++; error = "" }
                            .onFailure { error = it.message ?: "failed" }
                    }
                },
            )
        }
    }

    var issuing by remember { mutableStateOf(false) }
    TextButton(onClick = { issuing = true }) { Text("Issue a token") }

    if (issuing) {
        var newName by remember { mutableStateOf("") }
        AlertDialog(
            onDismissRequest = { issuing = false },
            title = { Text("Issue a token") },
            text = {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text(
                        "One name per producer — that is what will let you cut this " +
                            "one off without touching the others, and what the token " +
                            "itself will carry in front of its secret.",
                        style = MaterialTheme.typography.bodySmall,
                    )
                    OutlinedTextField(
                        value = newName,
                        onValueChange = { newName = it },
                        label = { Text("Producer name") },
                        placeholder = { Text("sonarr") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
            },
            confirmButton = {
                TextButton(
                    enabled = newName.isNotBlank(),
                    onClick = {
                        val name = newName.trim()
                        issuing = false
                        scope.launch {
                            createPublishToken(serverUrl, token, channelId, name)
                                .onSuccess { issued = it; reloads++; error = "" }
                                .onFailure { error = it.message ?: "creation failed" }
                        }
                    },
                ) { Text("Issue") }
            },
            dismissButton = { TextButton(onClick = { issuing = false }) { Text("Cancel") } },
        )
    }
}

/** What the phone refuses to send, matching what the server refuses to keep. */
private const val MAX_ICON_BYTES = 256 * 1024

/**
 * One producer.
 *
 * Its picture doubles as the button that changes it — tapping a logo to
 * replace a logo is the gesture one tries first — and the rest lives behind
 * an overflow, because a row with four text buttons side by side reads as a
 * toolbar rather than as a producer.
 */
@Composable
private fun TokenRow(
    producer: PublishTokenPayload,
    serverUrl: String,
    token: String,
    onPickIcon: () -> Unit,
    onRemoveIcon: () -> Unit,
    onRevoke: () -> Unit,
) {
    var menuOpen by remember { mutableStateOf(false) }

    Card(
        modifier = Modifier.fillMaxWidth(),
        border = BorderStroke(1.dp, MaterialTheme.colorScheme.outline),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth().padding(16.dp),
            horizontalArrangement = Arrangement.spacedBy(12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            IconButton(onClick = onPickIcon) {
                ProducerIcon(producer.id, producer.hasIcon, serverUrl, token)
            }

            Column(Modifier.weight(1f)) {
                Text(producer.name, style = MaterialTheme.typography.titleSmall)
                Text(
                    when {
                        producer.isRevoked -> "revoked"
                        producer.lastUsedAt != null -> "last published ${producer.lastUsedAt}"
                        // A token never used is often a misconfigured producer:
                        // say so rather than leave a blank.
                        else -> "never used"
                    },
                    style = MaterialTheme.typography.bodySmall,
                )
            }

            Box {
                IconButton(onClick = { menuOpen = true }) {
                    Icon(Icons.Default.MoreVert, contentDescription = "More")
                }
                DropdownMenu(expanded = menuOpen, onDismissRequest = { menuOpen = false }) {
                    DropdownMenuItem(
                        text = { Text(if (producer.hasIcon) "Replace the icon" else "Set an icon") },
                        onClick = { menuOpen = false; onPickIcon() },
                    )
                    if (producer.hasIcon) {
                        DropdownMenuItem(
                            text = { Text("Remove the icon") },
                            onClick = { menuOpen = false; onRemoveIcon() },
                        )
                    }
                    // The same entry does both steps: revoking cuts publishing
                    // and keeps the row, which says which producer was cut off
                    // and when; once that trace is useless, it is erased.
                    DropdownMenuItem(
                        text = { Text(if (producer.isRevoked) "Delete" else "Revoke") },
                        onClick = { menuOpen = false; onRevoke() },
                    )
                }
            }
        }
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
