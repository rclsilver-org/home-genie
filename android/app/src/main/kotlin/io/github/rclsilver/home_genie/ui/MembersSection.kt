package io.github.rclsilver.home_genie.ui

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.Box
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
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

/**
 * The rights, from the weakest to the strongest.
 *
 * They nest: administering implies publishing, publishing implies reading.
 * Only the highest one is shown — putting the three side by side suggested
 * three independent switches, two of which were lying.
 */
private val ROLES = listOf(
    "reader" to "Read",
    "writer" to "Publish",
    "owner" to "Administer",
)

/** What the role permits, in one line. */
private val ROLE_DETAIL = mapOf(
    "reader" to "reads the channel",
    "writer" to "reads and publishes",
    "owner" to "reads, publishes, and administers the channel",
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
        MemberCard(
            member = member,
            isOwner = isOwner,
            onRole = { role ->
                scope.launch {
                    setMember(serverUrl, token, channelId, member.username, role)
                        .onSuccess { reloads++; error = "" }
                        .onFailure { error = it.message ?: "change failed" }
                }
            },
            onRemove = {
                scope.launch {
                    removeMember(serverUrl, token, channelId, member.username)
                        .onSuccess { reloads++; error = "" }
                        // Removing the last owner is refused by the server:
                        // saying so here avoids the belief that the button is
                        // broken.
                        .onFailure { error = it.message ?: "removal failed" }
                }
            },
        )
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

/**
 * One member: who they are, and what they are allowed to do.
 *
 * The right shows as a single word, the highest one, and changes through a
 * menu — the gesture is rare and has consequences, so it is better asked
 * twice than once by accident.
 */
@Composable
private fun MemberCard(
    member: MemberPayload,
    isOwner: Boolean,
    onRole: (String) -> Unit,
    onRemove: () -> Unit,
) {
    var menuOpen by remember { mutableStateOf(false) }

    Card(
        modifier = Modifier.fillMaxWidth(),
        border = BorderStroke(1.dp, MaterialTheme.colorScheme.outline),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth().padding(16.dp),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(Modifier.weight(1f)) {
                Text(
                    member.displayName.ifEmpty { member.username },
                    style = MaterialTheme.typography.titleSmall,
                )
                Text(
                    buildString {
                        append(member.username)
                        append(" · ").append(ROLE_DETAIL[member.role] ?: member.role)
                    },
                    style = MaterialTheme.typography.bodySmall,
                )
            }

            Box {
                // The right is the button: one reads "Administer", and touches
                // the same word to change it.
                TextButton(
                    enabled = isOwner,
                    onClick = { menuOpen = true },
                ) {
                    Text(ROLES.toMap()[member.role] ?: member.role)
                }
                DropdownMenu(expanded = menuOpen, onDismissRequest = { menuOpen = false }) {
                    ROLES.forEach { (value, label) ->
                        DropdownMenuItem(
                            text = { Text(label) },
                            // A tick rather than a greyed-out line: the current
                            // right stays selectable, and showing it saves
                            // counting rows to know where one stands.
                            trailingIcon = {
                                if (member.role == value) {
                                    Icon(Icons.Default.Check, contentDescription = "current")
                                }
                            },
                            onClick = {
                                menuOpen = false
                                if (member.role != value) onRole(value)
                            },
                        )
                    }
                    HorizontalDivider()
                    DropdownMenuItem(
                        text = { Text("Remove from the channel") },
                        onClick = { menuOpen = false; onRemove() },
                    )
                }
            }
        }
    }
}
