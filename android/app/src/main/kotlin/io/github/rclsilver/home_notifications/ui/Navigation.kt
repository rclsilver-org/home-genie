package io.github.rclsilver.home_notifications.ui

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
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalDrawerSheet
import androidx.compose.material3.NavigationDrawerItem
import androidx.compose.material3.NavigationDrawerItemDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.unit.dp

/**
 * The application's sections, in menu order.
 *
 * A drawer rather than two rows of tabs: the alert console already has filters
 * of its own, and two stacked bars make one hesitate about which sorts what.
 * The drawer leaves the screen when it is not in use, which gives the room
 * back to the alerts themselves.
 */
enum class Destination(val label: String, val icon: ImageVector) {
    OVERVIEW("Overview", Icons.Default.Home),
    ALERTS("Alerts", Icons.Default.Warning),
    NOTIFICATIONS("Notifications", Icons.Default.Notifications),
    CHANNELS("Channels", Icons.Default.List),
    SETTINGS("Settings", Icons.Default.Settings),
    DIAGNOSTICS("Diagnostics", Icons.Default.Info),
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
                "home-notifications",
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
