package io.github.rclsilver.home_genie.ui

import android.graphics.BitmapFactory
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Info
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.luminance
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import io.github.rclsilver.home_genie.net.fetchProducerIcon
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

/**
 * The pictures already decoded, kept for the life of the process.
 *
 * A homelab has a handful of producers — Sonarr, Radarr, diun — so a feed of
 * fifty rows shows three distinct images. Caching by producer turns fifty
 * requests into three, and nothing here is large enough to want evicting.
 *
 * No image-loading library for that. Three small PNGs fetched through the
 * client already in the application, decoded once, is less code than
 * configuring one — and one dependency fewer in a repository that keeps them
 * inspectable.
 */
private object IconCache {
    private val lock = Mutex()
    private val decoded = mutableMapOf<Long, ImageBitmap?>()

    /**
     * Returns the icon, fetching it once. A failure is remembered as an
     * absence: a producer whose icon cannot be read must not have it asked
     * for again on every scroll.
     */
    suspend fun of(serverUrl: String, token: String, producerId: Long): ImageBitmap? {
        lock.withLock {
            if (decoded.containsKey(producerId)) return decoded[producerId]
        }

        val image = fetchProducerIcon(serverUrl, token, producerId)
            .mapCatching { bytes ->
                BitmapFactory.decodeByteArray(bytes, 0, bytes.size)?.asImageBitmap()
            }
            .getOrNull()

        lock.withLock { decoded[producerId] = image }
        return image
    }
}

/**
 * The face of whoever sent a notification.
 *
 * Reserves its place whether or not there is an image: a list where some rows
 * are indented and others are not reads as two lists, and the eye has to find
 * the left edge again on every row.
 */
@Composable
fun ProducerIcon(
    producerId: Long?,
    hasIcon: Boolean,
    serverUrl: String,
    token: String,
    size: Dp = 42.dp,
) {
    var image by remember(producerId) { mutableStateOf<ImageBitmap?>(null) }
    // Whether a picture is still on its way. The payload already says whether
    // there is one, so a producer known to have none shows its fallback at
    // once instead of flashing it and swapping a moment later.
    var pending by remember(producerId) { mutableStateOf(hasIcon && producerId != null) }

    LaunchedEffect(producerId, hasIcon, serverUrl, token) {
        image = if (producerId != null && hasIcon && serverUrl.isNotEmpty() && token.isNotEmpty()) {
            IconCache.of(serverUrl, token, producerId)
        } else null
        pending = false
    }

    Box(Modifier.size(size), contentAlignment = Alignment.Center) {
        when {
            image != null -> Box(
                Modifier
                    .size(size)
                    .clip(LOGO_SHAPE)
                    .background(logoGround())
                    .padding(4.dp),
                contentAlignment = Alignment.Center,
            ) {
                Image(
                    bitmap = image!!,
                    contentDescription = null,
                    contentScale = ContentScale.Fit,
                    modifier = Modifier.size(size),
                )
            }
            // Still loading: the space is reserved already, and an empty
            // circle for a fraction of a second is quieter than a wrong one.
            pending -> Unit
            else -> DefaultIcon(size)
        }
    }
}

/**
 * What a notification wears when its producer has no picture.
 *
 * An empty square read as something failing to load rather than as a producer
 * without a logo, and every notification that predates producers being
 * recorded has none — so this is the ordinary case, not the exception. The
 * glyph is deliberately neutral: it says "a notification", which is all that
 * is known, instead of claiming a sender.
 *
 * Drawn at the full size, because the glyph is itself a filled disc: it lands
 * on exactly the diameter the logos do. It sat on a tinted circle before,
 * which was invisible — the tint was `surfaceVariant`, the very colour an
 * unread card is painted — so all that showed was the glyph at two thirds of
 * the size, and the column of icons looked ragged.
 */
@Composable
private fun DefaultIcon(size: Dp) {
    Icon(
        Icons.Default.Info,
        contentDescription = null,
        tint = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.size(size),
    )
}

/**
 * The ground a producer's logo sits on.
 *
 * White under the dark theme, because these logos are drawn for a light one:
 * Radarr's is a dark outline around a yellow triangle, so on a dark card the
 * outline vanishes and only the middle survives. Giving each the ground its
 * designer assumed fixes them all at once, where finding a lighter variant
 * would fix Radarr and wait for the next producer to have the same trouble.
 *
 * Nothing at all under the light theme, where the card already is that
 * ground. Painting a near-white square there left a pale tile on every row —
 * it matched neither the read surface nor the unread one, so it read as a
 * third colour that answered nothing. Transparent is how a pastille says "the
 * same as whatever is behind me" without having to be told which.
 *
 * Read from the resolved scheme rather than from the system setting, so it
 * follows an explicit Light or Dark chosen in the application too.
 */
@Composable
private fun logoGround(): Color =
    if (MaterialTheme.colorScheme.surface.luminance() < 0.5f) Color.White
    else Color.Transparent

/**
 * The shape that ground is cut to: a rounded square, not a circle.
 *
 * A circle costs a square image its corners, and most of these are square
 * illustrations — diun's whale lost the bell in its top corner to it. A
 * rounded square keeps them and still reads as a badge rather than as a
 * picture dropped on the row. Sonarr's logo, which is itself a disc, sits in
 * one with a little air around it and is none the worse.
 */
private val LOGO_SHAPE = RoundedCornerShape(12.dp)
