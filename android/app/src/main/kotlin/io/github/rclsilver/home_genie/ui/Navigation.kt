package io.github.rclsilver.home_genie.ui

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.Info
import androidx.compose.material.icons.filled.List
import androidx.compose.material.icons.filled.Notifications
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material3.Badge
import androidx.compose.material3.BadgedBox
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalDrawerSheet
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.NavigationDrawerItem
import androidx.compose.material3.NavigationDrawerItemDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.unit.dp

/**
 * The application's sections.
 *
 * The four one moves between all day sit in a bar at the bottom, always a
 * thumb away. The overview is not one of them: it is read on arrival and then
 * left, so it stays in the drawer rather than spending a fifth of the bar to
 * be visited once.
 */
enum class Destination(val label: String, val icon: ImageVector) {
    DASHBOARD("Dashboard", Icons.Default.Home),
    ALERTS("Alerts", Icons.Default.Warning),
    NOTIFICATIONS("Notifications", Icons.Default.Notifications),
    CHANNELS("Channels", Icons.Default.List),
    SETTINGS("Settings", Icons.Default.Settings);

    companion object {
        /** What the bottom bar carries, in the order it carries them. */
        val primary = listOf(ALERTS, NOTIFICATIONS, CHANNELS, SETTINGS)
    }
}

/**
 * The bar at the bottom.
 *
 * It shows a count only where there is something to count. A badge at zero is
 * a permanent badge, and a permanent badge is one the eye learns to skip —
 * which is the one thing a count on an alert console must never become.
 *
 * The drawer keeps every section, this one included. A menu reduced to the
 * single item the bar does not carry would not be worth opening, and it is
 * also the only place that says who is signed in.
 */
@Composable
fun BottomBar(
    current: Destination,
    open: Int,
    unread: Int,
    onSelect: (Destination) -> Unit,
) {
    NavigationBar {
        Destination.primary.forEach { destination ->
            val count = when (destination) {
                Destination.ALERTS -> open
                Destination.NOTIFICATIONS -> unread
                else -> 0
            }
            NavigationBarItem(
                selected = destination == current,
                onClick = { onSelect(destination) },
                icon = {
                    BadgedBox(badge = {
                        if (count > 0) Badge { Text(if (count > 99) "99+" else "$count") }
                    }) {
                        Icon(destination.icon, contentDescription = null)
                    }
                },
                label = { Text(destination.label) },
            )
        }
    }
}

/**
 * The drawer's content.
 *
 * [unread] only shows when there is something to read: a badge at zero is a
 * permanent badge, and a permanent badge is one the eye learns to skip.
 */
@Composable
fun DrawerContent(
    current: Destination,
    unread: Int,
    username: String,
    onSelect: (Destination) -> Unit,
) {
    ModalDrawerSheet {
        Column(
            Modifier
                .padding(horizontal = 12.dp)
                .verticalScroll(rememberScrollState())
        ) {
            Text(
                "Home Genie",
                style = MaterialTheme.typography.titleMedium,
                modifier = Modifier.padding(16.dp),
            )
            if (username.isNotEmpty()) {
                Text(
                    username,
                    style = MaterialTheme.typography.bodySmall,
                    modifier = Modifier.padding(start = 16.dp, bottom = 8.dp),
                )
            }
            HorizontalDivider()
            // The first item must not touch the divider: its selection pill is
            // a solid fill, and a solid fill against a line reads as a smudge
            // rather than as a state.
            Spacer(Modifier.height(8.dp))

            Destination.entries.forEach { destination ->
                NavigationDrawerItem(
                    label = { Text(destination.label) },
                    icon = { Icon(destination.icon, contentDescription = null) },
                    badge = {
                        if (destination == Destination.NOTIFICATIONS && unread > 0) {
                            Badge { Text(if (unread > 99) "99+" else "$unread") }
                        }
                    },
                    selected = destination == current,
                    onClick = { onSelect(destination) },
                    modifier = Modifier.padding(NavigationDrawerItemDefaults.ItemPadding),
                )
            }
        }
    }
}
