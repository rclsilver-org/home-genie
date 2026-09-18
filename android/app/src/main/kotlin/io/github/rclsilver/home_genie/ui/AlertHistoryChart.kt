package io.github.rclsilver.home_genie.ui

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.unit.dp
import io.github.rclsilver.home_genie.net.HistoryBucketPayload

/**
 * How many alerts opened, over time.
 *
 * Drawn by hand rather than pulled in: a bar chart is a loop over a list and
 * two rectangles, and a charting library would arrive with a theme of its own
 * to argue with.
 *
 * The critical share is stacked at the foot of each column rather than given
 * a chart of its own. What one looks for here is *when* a bad night happened,
 * and a second chart underneath would make that a comparison instead of a
 * glance.
 */
@Composable
fun AlertHistoryChart(
    buckets: List<HistoryBucketPayload>,
    hours: Int,
    modifier: Modifier = Modifier,
    height: androidx.compose.ui.unit.Dp = 120.dp,
) {
    if (buckets.isEmpty()) return

    val tallest = buckets.maxOf { it.total }
    val ordinary = MaterialTheme.colorScheme.primary
    val alarming = MaterialTheme.colorScheme.error
    val axis = MaterialTheme.colorScheme.outlineVariant

    Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        // The scale, where a scale goes. Unlabelled bars say "something
        // happened" and leave the reader to guess how much.
        Text(
            if (tallest == 0) "nothing in this window" else "peak $tallest",
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Canvas(modifier.fillMaxWidth().height(height)) {
            val gap = size.width / buckets.size * 0.25f
            val column = size.width / buckets.size - gap

            // A floor of one, so a quiet week draws a flat baseline instead of
            // dividing by zero — and so a single alert does not fill the frame
            // and read as a storm.
            val ceiling = maxOf(tallest, 1)

            drawLine(
                color = axis,
                start = Offset(0f, size.height),
                end = Offset(size.width, size.height),
                strokeWidth = 1f,
            )

            buckets.forEachIndexed { index, bucket ->
                if (bucket.total == 0) return@forEachIndexed
                val left = index * (column + gap)
                val full = size.height * bucket.total / ceiling
                val critical = size.height * bucket.critical / ceiling

                drawRect(
                    color = ordinary,
                    topLeft = Offset(left, size.height - full),
                    size = Size(column, full - critical),
                )
                if (bucket.critical > 0) {
                    drawRect(
                        color = alarming,
                        topLeft = Offset(left, size.height - critical),
                        size = Size(column, critical),
                    )
                }
            }
        }

        // Only the span on the axis, one label at each end. The peak belongs
        // above: between two time labels it reads as a value at that moment,
        // which is the one thing it is not.
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.SpaceBetween,
        ) {
            Text(
                spanOf(hours),
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Text(
                "now",
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

/**
 * The left edge of the axis, named from the window the caller asked for.
 *
 * Told rather than guessed from the number of columns: the same twenty-four
 * columns can be a day or a month, and a chart that says the wrong one is
 * worse than one that says nothing.
 */
private fun spanOf(hours: Int): String = when {
    hours % 24 == 0 && hours >= 48 -> "${hours / 24} days ago"
    hours == 24 -> "a day ago"
    hours == 1 -> "an hour ago"
    else -> "$hours hours ago"
}
