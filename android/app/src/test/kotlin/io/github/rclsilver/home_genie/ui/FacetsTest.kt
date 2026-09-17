package io.github.rclsilver.home_genie.ui

import io.github.rclsilver.home_genie.net.AlertPayload
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

private fun alert(
    id: Long = 1,
    channel: String = "alerts",
    labels: Map<String, String> = emptyMap(),
) = AlertPayload(id = id, channelId = 1, channelSlug = channel, labels = labels)

class FacetsTest {
    /**
     * The shape of the real server: one alert channel, one bridge. Both were
     * printed on every row and neither told the reader anything.
     */
    @Test
    fun `a facet shared by every alert is dropped`() {
        val facets = facetsOf(
            listOf(
                alert(1, labels = mapOf("source" to "icinga", "service" to "speedtest")),
                alert(2, labels = mapOf("source" to "icinga", "service" to "puppet-agent")),
            )
        )

        assertFalse("one channel everywhere", facets.channel)
        assertEquals(setOf("service"), facets.labels)
    }

    @Test
    fun `a facet that varies is kept`() {
        val facets = facetsOf(
            listOf(
                alert(1, channel = "infra", labels = mapOf("service" to "cpu")),
                alert(2, channel = "media", labels = mapOf("service" to "sonarr")),
            )
        )

        assertTrue(facets.channel)
        assertEquals(setOf("service"), facets.labels)
    }

    /** A label on some rows and not others differs by its very absence. */
    @Test
    fun `a label missing from some alerts counts as varying`() {
        val facets = facetsOf(
            listOf(
                alert(1, labels = mapOf("service" to "cpu")),
                alert(2, labels = emptyMap()),
            )
        )

        assertEquals(setOf("service"), facets.labels)
    }

    /** Nothing to repeat against, so nothing is hidden. */
    @Test
    fun `a single alert keeps everything`() {
        val facets = facetsOf(listOf(alert(1, labels = mapOf("source" to "icinga"))))

        assertTrue(facets.channel)
        assertEquals(setOf("source"), facets.labels)
    }

    @Test
    fun `the empty list asks for nothing`() {
        val facets = facetsOf(emptyList())

        assertTrue(facets.labels.isEmpty())
        assertEquals("", facets.sharedDomain)
    }

    @Test
    fun `the domain every host shares is found`() {
        assertEquals(
            ".chatillon.betrancourt.net",
            sharedDomain(
                listOf(
                    "unifi-router.chatillon.betrancourt.net",
                    "wmbusmeters.chatillon.betrancourt.net",
                )
            ),
        )
    }

    @Test
    fun `hosts with nothing in common keep their names`() {
        assertEquals("", sharedDomain(listOf("backup-01", "nas-01")))
        assertEquals("", sharedDomain(listOf("a.example.net", "b.example.org")))
    }

    /**
     * Trimming must leave something to read. Two hosts under one domain where
     * one *is* the domain would otherwise be reduced to nothing.
     */
    @Test
    fun `a name is never trimmed away entirely`() {
        val shared = sharedDomain(listOf("example.net", "host.example.net"))

        assertEquals(".net", shared)
        assertEquals("example", "example.net".removeSuffix(shared))
        assertEquals("host.example", "host.example.net".removeSuffix(shared))
    }

    /** Removing a suffix they all carry cannot make two names collide. */
    @Test
    fun `trimming keeps distinct hosts distinct`() {
        val names = listOf("a.lan.example.net", "b.lan.example.net", "c.other.example.net")
        val shared = sharedDomain(names)
        val trimmed = names.map { it.removeSuffix(shared) }

        assertEquals(".example.net", shared)
        assertEquals(names.size, trimmed.toSet().size)
    }
}
