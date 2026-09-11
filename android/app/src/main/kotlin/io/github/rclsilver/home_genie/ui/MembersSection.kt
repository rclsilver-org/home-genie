package io.github.rclsilver.home_genie.ui

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.Box
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.ListItem
import androidx.compose.material3.ListItemDefaults
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Card
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
import io.github.rclsilver.home_genie.net.UserSuggestionPayload
import io.github.rclsilver.home_genie.net.searchUsers
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

    var adding by remember { mutableStateOf(false) }
    TextButton(onClick = { adding = true }) { Text("Add a member") }

    if (adding) {
        AddMemberDialog(
            serverUrl = serverUrl,
            token = token,
            existing = members.map { it.username }.toSet(),
            onDismiss = { adding = false },
            onAdd = { username, role ->
                adding = false
                scope.launch {
                    setMember(serverUrl, token, channelId, username, role)
                        .onSuccess { reloads++; error = "" }
                        .onFailure { error = it.message ?: "add failed" }
                }
            },
        )
    }
}

/**
 * The add form, in a dialog.
 *
 * It used to live permanently under the list, taking a third of the screen
 * for a gesture made once per channel. Behind a button, the member list goes
 * back to being what one comes to read.
 *
 * The name is picked rather than spelled: a mistyped username only produces
 * an "unknown user" after the fact, while the accounts are known to the
 * server and can be counted on one hand.
 */
@Composable
private fun AddMemberDialog(
    serverUrl: String,
    token: String,
    existing: Set<String>,
    onDismiss: () -> Unit,
    onAdd: (String, String) -> Unit,
) {
    var query by remember { mutableStateOf("") }
    var role by remember { mutableStateOf("reader") }
    var candidates by remember { mutableStateOf<List<UserSuggestionPayload>>(emptyList()) }
    var chosen by remember { mutableStateOf<String?>(null) }

    // The whole list shows before the first keystroke: on a handful of
    // accounts, searching usually means scrolling.
    LaunchedEffect(query) {
        searchUsers(serverUrl, token, query).onSuccess { candidates = it }
    }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Add a member") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(
                    value = query,
                    onValueChange = { query = it; chosen = null },
                    label = { Text("Username") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )

                Column(
                    Modifier.heightIn(max = 220.dp).verticalScroll(rememberScrollState()),
                ) {
                    candidates.forEach { candidate ->
                        // Members already in the channel stay visible but greyed
                        // out: hiding them would cast doubt on the search.
                        val already = candidate.username in existing
                        ListItem(
                            headlineContent = {
                                Text(candidate.displayName.ifEmpty { candidate.username })
                            },
                            supportingContent = {
                                Text(
                                    if (already) "${candidate.username} · already a member"
                                    else candidate.username
                                )
                            },
                            trailingContent = {
                                if (chosen == candidate.username) {
                                    Icon(Icons.Default.Check, contentDescription = "selected")
                                }
                            },
                            colors = ListItemDefaults.colors(
                                containerColor = if (chosen == candidate.username)
                                    MaterialTheme.colorScheme.secondaryContainer
                                else MaterialTheme.colorScheme.surface,
                            ),
                            modifier = Modifier.clickable(enabled = !already) {
                                chosen = candidate.username
                                query = candidate.username
                            },
                        )
                    }
                    if (candidates.isEmpty()) {
                        Text("No account matches.",
                            style = MaterialTheme.typography.bodySmall)
                    }
                }

                // The same gesture as on a member card: one word, and the menu
                // that changes it. Three chips do not fit the width of a
                // dialog, and "Administer" folded into a column of letters.
                var roleMenu by remember { mutableStateOf(false) }
                Box {
                    TextButton(onClick = { roleMenu = true }) {
                        Text("Right: ${ROLES.toMap()[role] ?: role}")
                    }
                    DropdownMenu(expanded = roleMenu, onDismissRequest = { roleMenu = false }) {
                        ROLES.forEach { (value, label) ->
                            DropdownMenuItem(
                                text = { Text(label) },
                                trailingIcon = {
                                    if (role == value) {
                                        Icon(Icons.Default.Check, contentDescription = "current")
                                    }
                                },
                                onClick = { role = value; roleMenu = false },
                            )
                        }
                    }
                }

            }
        },
        confirmButton = {
            val target = chosen ?: query.trim()
            TextButton(
                enabled = target.isNotBlank() && target !in existing,
                onClick = { onAdd(target, role) },
            ) { Text("Add") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
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
