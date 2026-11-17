package io.github.rclsilver.home_genie.ui

/** Commit on release after a left drag of at least half the measured row. */
internal fun shouldAcknowledgeAlertSwipe(offset: Float, width: Float): Boolean =
    width > 0f && offset <= -width * 0.5f
