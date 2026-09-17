package io.github.rclsilver.home_genie.ui

import io.github.rclsilver.home_genie.net.AlertPayload

/**
 * Which facets of an alert are worth the width they take on a row.
 *
 * A facet that reads the same on every row costs space and tells nobody
 * anything. On a server with a single alert channel the channel is on all of
 * them; when every alert arrives through one bridge, so is `source`. Both
 * were being shown, and both were noise.
 *
 * Computed from what is on screen rather than configured, so it follows the
 * data: split the channels one day and the channel comes back on its own.
 */
data class Facets(
    /** Whether the channel differs between the alerts on screen. */
    val channel: Boolean,
    /** The label keys whose value differs between them. */
    val labels: Set<String>,
    /** The domain every instance shares, ready to be trimmed off. */
    val sharedDomain: String,
)

/**
 * Decides what to show for a given list.
 *
 * A single alert hides nothing: with no second row to repeat it, a facet
 * cannot be redundant, and the reader is better served by all of it.
 */
fun facetsOf(alerts: List<AlertPayload>): Facets {
    if (alerts.size < 2) {
        return Facets(
            channel = true,
            labels = alerts.firstOrNull()?.labels?.keys.orEmpty(),
            sharedDomain = "",
        )
    }

    // A key missing from some alerts counts as varying: its absence is itself
    // the difference between two rows.
    val keys = alerts.flatMap { it.labels.keys }.toSet()
    val varying = keys.filterTo(mutableSetOf()) { key ->
        alerts.mapTo(mutableSetOf()) { it.labels[key] }.size > 1
    }

    return Facets(
        channel = alerts.mapTo(mutableSetOf()) { it.channelSlug }.size > 1,
        labels = varying,
        sharedDomain = sharedDomain(alerts.mapNotNull { it.labels["instance"] }),
    )
}

/**
 * The trailing domain every name shares, dot included, or "" if they share
 * none.
 *
 * `unifi-router.chatillon.betrancourt.net` beside twenty siblings says one
 * useful word and then repeats the house it lives in. Removing a suffix they
 * all carry can never make two names collide, since what differs is what
 * comes before it.
 *
 * The whole name is never taken: a row reading nothing at all would be worse
 * than one reading too much.
 */
fun sharedDomain(names: List<String>): String {
    if (names.size < 2) return ""
    val parts = names.map { it.split('.').asReversed() }
    var shared = 0
    while (parts.all { it.size > shared + 1 }) {
        val here = parts.first()[shared]
        if (parts.any { it[shared] != here }) break
        shared++
    }
    if (shared == 0) return ""
    return "." + parts.first().take(shared).asReversed().joinToString(".")
}
