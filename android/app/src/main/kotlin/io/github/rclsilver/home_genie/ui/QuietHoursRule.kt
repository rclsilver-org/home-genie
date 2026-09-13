package io.github.rclsilver.home_genie.ui

import io.github.rclsilver.home_genie.net.QuietHoursPayload

/** The window a card shows, and where it comes from. */
data class ResolvedQuietHours(
    val window: QuietHoursPayload?,
    /**
     * The window belongs to the default and the card is showing it to a
     * channel that has not overridden it. Only ever true in a channel.
     */
    val inherited: Boolean,
)

/**
 * Decides what one severity's card shows.
 *
 * The most specific wins: a channel's own window hides the default.
 *
 * Inheritance, though, is a question only a channel can ask. The settings
 * screen *is* the default, so everything it lists is its own — even though
 * the server tags those rows `default`, which says where a row lives, not
 * that someone else set it. Reading that tag as "inherited" everywhere made
 * the settings screen offer to override a channel it was never editing.
 */
fun resolveQuietHours(
    windows: List<QuietHoursPayload>,
    severity: String,
    inChannel: Boolean,
): ResolvedQuietHours {
    val own = windows.firstOrNull { it.severity == severity && it.isOverride }
    val default = windows.firstOrNull { it.severity == severity && !it.isOverride }
    return ResolvedQuietHours(
        window = own ?: default,
        inherited = inChannel && own == null && default != null,
    )
}
