package io.github.rclsilver.home_genie.ui

import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.LinkAnnotation
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.TextLinkStyles
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.withLink
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import io.github.rclsilver.home_genie.net.MessagePayload

/**
 * One notification, in full.
 *
 * Opening it used to mean reading it, and nothing else: the row marked
 * itself read under the thumb and gave nothing back. That was the wrong
 * trade for objects that carry a link — the one thing a *arr notification is
 * usually for — and for bodies long enough to be cut in a list.
 *
 * So a tap opens, and a full swipe reads. The gesture that loses the message
 * from the unread list is now the deliberate one.
 */
@Composable
fun NotificationDetailScreen(message: MessagePayload, serverUrl: String, token: String) {
    val context = LocalContext.current

    Text(message.title.ifEmpty { message.channelSlug }, style = MaterialTheme.typography.titleLarge)

    Row(
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            buildString {
                append(message.channelSlug)
                if (message.createdAt.isNotEmpty()) {
                    append(" · ").append(relativeAge(message.createdAt))
                }
            },
            style = MaterialTheme.typography.bodySmall,
        )
        TagChips(message.tags, max = 6)
    }

    // The whole body, wrapped rather than cut. The list gives one line and
    // this screen exists for the rest of it.
    if (message.body.isNotEmpty()) {
        Card(Modifier.fillMaxWidth()) {
            Text(
                message.body,
                style = MaterialTheme.typography.bodyMedium,
                modifier = Modifier.padding(16.dp),
            )
        }
    }

    // The link a producer attached. It has been carried in the payload all
    // along and shown nowhere, so a notification pointing at a film or a
    // dashboard arrived with its destination unreachable.
    val link = message.clickUrl
    if (link.isNotEmpty()) {
        Button(
            modifier = Modifier.fillMaxWidth(),
            onClick = {
                runCatching {
                    context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(link)))
                }
            },
        ) { Text("Open link") }
        Text(link, style = MaterialTheme.typography.labelSmall)
    }

    // Expanded, not behind a toggle: on this screen the question has already
    // been asked by opening it.
    Text("Delivery", style = MaterialTheme.typography.titleSmall)
    Column(Modifier.fillMaxWidth()) {
        TimelinePanel(message.id, serverUrl, token)
    }
}

