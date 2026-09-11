package io.github.rclsilver.home_genie.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Card
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import io.github.rclsilver.home_genie.net.MemberPayload
import io.github.rclsilver.home_genie.net.fetchMembers
import io.github.rclsilver.home_genie.net.removeMember
import io.github.rclsilver.home_genie.net.setMember
import io.github.rclsilver.home_genie.service.ConnectionService

/** The server's roles, with what they actually allow. */
private val ROLES = listOf(
    "reader" to "read",
    "writer" to "read and publish",
    "owner" to "administer",
)

/**
 * A channel's members and their rights.
 *
 * Sharing a channel used to be a `curl` operation — precisely what one does
 * not do to show the mediacenter notifications to somebody in the house.
 */
@Composable
fun MembersSection(channelId: Long, serverUrl: String, token: String, isOwner: Boolean) {
    val scope = rememberCoroutineScope()
    var members by remember { mutableStateOf<List<MemberPayload>>(emptyList()) }
    var username by remember { mutableStateOf("") }
    var role by remember { mutableStateOf("reader") }
    var error by remember { mutableStateOf("") }
    var reloads by remember { mutableStateOf(0) }

    // The server broadcasts "member.changed": a share made from another
    // device must show up here without reopening the channel.
    val state by ConnectionService.observedState.collectAsState()
    LaunchedEffect(channelId, reloads, state.events) {
        fetchMembers(serverUrl, token, channelId)
            .onSuccess { members = it; error = "" }
            .onFailure { error = it.message ?: "" }
    }

    Text("Members", style = MaterialTheme.typography.titleMedium)
    if (error.isNotEmpty()) {
        Text(error, color = MaterialTheme.colorScheme.error)
    }

    members.sortedBy { it.username }.forEach { member ->
        Card(Modifier.fillMaxWidth()) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Column {
                        Text(
                            member.displayName.ifEmpty { member.username },
                            style = MaterialTheme.typography.titleSmall,
                        )
                        Text(member.username, style = MaterialTheme.typography.bodySmall)
                    }
                    if (isOwner) {
                        TextButton(onClick = {
                            scope.launch {
                                removeMember(serverUrl, token, channelId, member.username)
                                    .onSuccess { reloads++; error = "" }
                                    // Removing the last owner is refused by the
                                    // server: saying so here avoids believing
                                    // the button is broken.
                                    .onFailure { error = it.message ?: "retrait impossible" }
                            }
                        }) { Text("Retirer") }
                    }
                }

                if (isOwner) {
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        ROLES.forEach { (value, label) ->
                            FilterChip(
                                selected = member.role == value,
                                onClick = {
                                    if (member.role != value) {
                                        scope.launch {
                                            setMember(serverUrl, token, channelId,
                                                member.username, value)
                                                .onSuccess { reloads++; error = "" }
                                                .onFailure {
                                                    error = it.message ?: "changement impossible"
                                                }
                                        }
                                    }
                                },
                                label = { Text(label) },
                            )
                        }
                    }
                } else {
                    Text(
                        ROLES.toMap()[member.role] ?: member.role,
                        style = MaterialTheme.typography.bodySmall,
                    )
                }
            }
        }
    }

    // A non-owner sees who shares the channel with them but cannot change it:
    // the server would refuse anyway, so the gesture is not offered.
    if (!isOwner) return

    OutlinedTextField(
        value = username,
        onValueChange = { username = it },
        label = { Text("Username") },
        singleLine = true,
        modifier = Modifier.fillMaxWidth(),
    )
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        ROLES.forEach { (value, label) ->
            FilterChip(
                selected = role == value,
                onClick = { role = value },
                label = { Text(label) },
            )
        }
    }
    TextButton(
        enabled = username.isNotBlank(),
        onClick = {
            scope.launch {
                setMember(serverUrl, token, channelId, username.trim(), role)
                    .onSuccess { username = ""; reloads++; error = "" }
                    .onFailure { error = it.message ?: "ajout impossible" }
            }
        },
    ) { Text("Ajouter") }
}
