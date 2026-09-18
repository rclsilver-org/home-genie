package io.github.rclsilver.home_genie.ui

import androidx.compose.animation.core.Animatable
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.detectHorizontalDragGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.Row
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.RectangleShape
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlin.math.roundToInt
import kotlinx.coroutines.launch
import io.github.rclsilver.home_genie.net.fetchFeed
import io.github.rclsilver.home_genie.net.markFeedRead
import io.github.rclsilver.home_genie.net.markRead
import io.github.rclsilver.home_genie.net.MessagePayload
import io.github.rclsilver.home_genie.service.ConnectionService

/**
 * The notification feed: what the media tools and the image watcher publish.
 *
 * Separate from the alert console because these objects do not live the
 * same way. A notification is read or unread, per person, and nothing else
 * ever happens to it; an alert opens, is taken, reminds and closes, for
 * everybody at once. Mixing them would force each to borrow the other's
 * vocabulary.
 *
 * Reading is personal: emptying this view empties it for nobody else —
 * which is the whole point of a shared feed.
 */
@Composable
fun NotificationsScreen(
    serverUrl: String,
    token: String,
    onOpen: (MessagePayload) -> Unit,
) {
    val scope = rememberCoroutineScope()
    // Unread by default: the view exists to be emptied.
    var unreadOnly by remember { mutableStateOf(true) }
    var all by remember { mutableStateOf<List<MessagePayload>>(emptyList()) }
    var capped by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf("") }
    var reloads by remember { mutableStateOf(0) }

    val state by ConnectionService.observedState.collectAsState()
    // One fetch for both tabs, as on the alert console: switching is instant,
    // and each tab can say how much it holds without a round trip.
    LaunchedEffect(state.events, reloads) {
        fetchFeed(serverUrl, token, unreadOnly = false, limit = FEED_LIMIT)
            .onSuccess { all = it; capped = it.size >= FEED_LIMIT; error = "" }
            .onFailure { error = it.message ?: "loading failed" }
    }

    val unread = all.filter { !it.read }
    val messages = if (unreadOnly) unread else all
    fun count(n: Int) = if (capped) "$n+" else "$n"

    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        FilterChip(
            selected = unreadOnly,
            onClick = { unreadOnly = true },
            label = { Text("Unread  ${count(unread.size)}") },
        )
        FilterChip(
            selected = !unreadOnly,
            onClick = { unreadOnly = false },
            label = { Text("All  ${count(all.size)}") },
        )
        if (unread.isNotEmpty()) {
            TextButton(onClick = {
                scope.launch { markFeedRead(serverUrl, token).onSuccess { reloads++ } }
            }) { Text("Mark all read") }
        }
    }

    if (error.isNotEmpty()) {
        Text(error, color = MaterialTheme.colorScheme.error)
    }

    if (messages.isEmpty()) {
        Text(
            if (unreadOnly) "Nothing new." else "No notification.",
            style = MaterialTheme.typography.bodyLarge,
        )
        return
    }

    messages.forEach { message ->
        // Keyed by message. Without it the rows are matched by position, so the
        // one that leaves the list hands its slot — and the gesture still
        // attached to it — to whichever moves up. The alert console carries
        // the same key for the same reason.
        key(message.id) {
            NotificationRow(
                message = message,
                serverUrl = serverUrl,
                token = token,
                onOpen = { onOpen(message) },
                onRead = {
                    scope.launch {
                        markRead(serverUrl, token, message.id).onSuccess { reloads++ }
                    }
                },
            )
        }
    }
}

/**
 * How much of the feed the screen holds at once.
 *
 * As on the alert console, the counts on the chips are computed here because
 * the server has no endpoint returning them, and that is only honest while
 * the whole set is in hand. The server caps this list, so once the cap is
 * reached a count is a floor and the chip says so.
 */
private const val FEED_LIMIT = 200

/**
 * How far the card must travel before releasing it counts as reading.
 *
 * Half the row: far enough that a scroll that wanders sideways does not
 * empty the feed, near enough that the gesture does not have to be finished
 * perfectly. Nothing is revealed on the way — the swipe *is* the action, so
 * there is no button to aim at.
 */
private const val READ_THRESHOLD = 0.5f

@Composable
private fun NotificationRow(
    message: MessagePayload,
    serverUrl: String,
    token: String,
    onOpen: () -> Unit,
    onRead: () -> Unit,
) {
    // Nothing to do to a message already read: it does not move.
    val swipeable = !message.read
    val drag = rememberCoroutineScope()
    val offset = remember(message.id) { Animatable(0f) }
    var width by remember(message.id) { mutableStateOf(0f) }
    val outline = MaterialTheme.colorScheme.outline

    Box(
        Modifier
            .fillMaxWidth()
            .clip(ROW_SHAPE)
            .onSizeChanged { width = it.width.toFloat() }
            // The outline belongs to the row, not to the card sliding inside
            // it, and is drawn after its children so nothing can cover it.
            .drawWithContent {
                drawContent()
                val w = 1.dp.toPx()
                drawRoundRect(
                    color = outline,
                    topLeft = Offset(w / 2, w / 2),
                    size = Size(size.width - w, size.height - w),
                    cornerRadius = CornerRadius(ROW_RADIUS.toPx()),
                    style = Stroke(w),
                )
            }
    ) {
        if (swipeable) {
            // What the card uncovers says what letting go will do. It is not
            // a target: the release commits, so there is nothing to press.
            Box(
                Modifier
                    .matchParentSize()
                    .background(MaterialTheme.colorScheme.primary),
                contentAlignment = Alignment.CenterEnd,
            ) {
                Text(
                    "Read",
                    style = MaterialTheme.typography.labelLarge,
                    color = MaterialTheme.colorScheme.onPrimary,
                    modifier = Modifier.padding(end = 24.dp),
                )
            }
        }

        Card(
            modifier = Modifier
                .fillMaxWidth()
                .offset { IntOffset(offset.value.roundToInt(), 0) }
                // Keyed on the message too: a pointerInput block is not
                // restarted while its key holds, so one keyed on `swipeable`
                // alone kept the previous row's animation and its onRead —
                // which is why a second swipe did nothing at all.
                .pointerInput(message.id, swipeable) {
                    if (!swipeable) return@pointerInput
                    detectHorizontalDragGestures(
                        onHorizontalDrag = { change, delta ->
                            change.consume()
                            drag.launch {
                                offset.snapTo((offset.value + delta).coerceIn(-width, 0f))
                            }
                        },
                        onDragEnd = {
                            val far = width > 0f && offset.value < -width * READ_THRESHOLD
                            // Home either way: read, the row leaves the unread
                            // list on its own; not read, it was never meant to
                            // stay open, since there is nothing behind to press.
                            drag.launch { offset.animateTo(0f) }
                            if (far) onRead()
                        },
                    )
                },
            colors = CardDefaults.cardColors(
                containerColor = if (message.read) MaterialTheme.colorScheme.surface
                else MaterialTheme.colorScheme.surfaceVariant,
            ),
            // Square, and with no border: both belong to the row.
            shape = RectangleShape,
            // A tap opens rather than reads. These carry links and bodies too
            // long for a list, and the old gesture spent the message to show
            // nothing of it.
            onClick = onOpen,
        ) {
            Row(
                Modifier.padding(16.dp),
                horizontalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                // Whoever sent it, at the left edge. A publish token is one per
                // software, so the token that published a message is its
                // producer — it only lacked a face.
                ProducerIcon(message.producerId, message.producerIcon, serverUrl, token)

                Column(
                    Modifier.weight(1f),
                    verticalArrangement = Arrangement.spacedBy(4.dp),
                ) {
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text(
                            message.title.ifEmpty { message.channelSlug },
                            style = MaterialTheme.typography.titleSmall,
                            fontWeight = if (message.read) FontWeight.Normal else FontWeight.Bold,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                            modifier = Modifier.weight(1f),
                        )
                        // The age, right where a feed is read for it. Its absence
                        // was not a style choice: the field was never declared on
                        // the payload, so two minutes old and two days old looked
                        // alike.
                        if (message.createdAt.isNotEmpty()) {
                            Text(
                                relativeAge(message.createdAt),
                                style = MaterialTheme.typography.labelSmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    }
        
                    if (message.body.isNotEmpty()) {
                        // Stripped, not rendered: two lines have to carry the message
                        // and every bracket spent here is a word lost.
                        Text(
                            plainText(message.body),
                            style = MaterialTheme.typography.bodyMedium,
                            maxLines = 2,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
        
                    // No channel here. Which one a notification came through is
                    // a property of the feed, not of the message: it reads the
                    // same on every row of a feed one has chosen to open, and
                    // the detail screen states it for the one case where it
                    // matters.
                    TagChips(message.tags)
                }
            }
        }
    }
}
