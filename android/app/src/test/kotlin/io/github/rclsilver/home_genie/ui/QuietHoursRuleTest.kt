package io.github.rclsilver.home_genie.ui

import io.github.rclsilver.home_genie.net.QuietHoursPayload
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

private fun default(severity: String = "", from: String = "22:00", to: String = "08:00") =
    QuietHoursPayload(severity = severity, from = from, to = to, scope = "default")

private fun override(severity: String = "", from: String = "23:00", to: String = "07:00") =
    QuietHoursPayload(severity = severity, from = from, to = to, scope = "channel")

class QuietHoursRuleTest {
    /**
     * The settings screen is the default. Its rows come back tagged `default`
     * because that is where they live, which once made it announce them as
     * inherited and offer to override a channel it was never editing.
     */
    @Test
    fun `the settings screen owns its windows rather than inheriting them`() {
        val resolved = resolveQuietHours(listOf(default()), severity = "", inChannel = false)

        assertFalse(resolved.inherited)
        assertEquals("22:00", resolved.window?.from)
    }

    @Test
    fun `a channel with no window of its own inherits the default`() {
        val resolved = resolveQuietHours(listOf(default()), severity = "", inChannel = true)

        assertTrue(resolved.inherited)
        assertEquals("22:00", resolved.window?.from)
    }

    @Test
    fun `a channel's own window hides the default`() {
        val resolved = resolveQuietHours(
            listOf(default(), override()), severity = "", inChannel = true,
        )

        assertFalse(resolved.inherited)
        assertEquals("23:00", resolved.window?.from)
    }

    @Test
    fun `a severity with no window anywhere shows nothing to inherit`() {
        val resolved = resolveQuietHours(listOf(default()), severity = "critical", inChannel = true)

        assertNull(resolved.window)
        assertFalse(resolved.inherited)
    }

    /** Each severity is resolved on its own; the broad window is not a fallback. */
    @Test
    fun `a window is matched on its own severity`() {
        val windows = listOf(default(), default(severity = "critical", from = "01:00", to = "06:00"))

        assertEquals("01:00", resolveQuietHours(windows, "critical", inChannel = false).window?.from)
        assertEquals("22:00", resolveQuietHours(windows, "", inChannel = false).window?.from)
    }
}
