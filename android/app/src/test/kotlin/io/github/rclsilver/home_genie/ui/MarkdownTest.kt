package io.github.rclsilver.home_genie.ui

import org.junit.Assert.assertEquals
import org.junit.Test

/** What Diun actually sends, copied from a message this server received. */
private const val DIUN =
    "Docker tag [**ghcr.io/mealie-recipes/mealie:latest**]" +
        "(https://github.com/mealie-recipes/mealie) which you subscribed to " +
        "through kubernetes provider has been updated on ghcr.io registry."

class MarkdownTest {
    @Test
    fun `the link Diun buries in its body is found`() {
        assertEquals("https://github.com/mealie-recipes/mealie", firstLink(DIUN))
    }

    @Test
    fun `the list reads the sentence without its markers`() {
        assertEquals(
            "Docker tag ghcr.io/mealie-recipes/mealie:latest which you subscribed to " +
                "through kubernetes provider has been updated on ghcr.io registry.",
            plainText(DIUN),
        )
    }

    /** `[**text**](url)` — the label is bold inside the link. */
    @Test
    fun `a bold label inside a link keeps only its words`() {
        val spans = parseInline("see [**the thing**](https://example.net) now")

        assertEquals(
            listOf(
                Span.Plain("see "),
                Span.Link("the thing", "https://example.net"),
                Span.Plain(" now"),
            ),
            spans,
        )
    }

    @Test
    fun `bold on its own is understood`() {
        assertEquals(listOf(Span.Bold("loud")), parseInline("**loud**"))
    }

    /**
     * The point of refusing to guess: a producer writing about `a * b` or
     * shell globs must get its characters back exactly as it sent them.
     */
    @Test
    fun `an unmatched marker is left alone`() {
        assertEquals("2 * 3 = 6", plainText("2 * 3 = 6"))
        assertEquals("a [bracket] and (parens)", plainText("a [bracket] and (parens)"))
        assertEquals("**", plainText("**"))
    }

    @Test
    fun `a body with nothing to render survives untouched`() {
        val body = "Star Trek: Strange New Worlds - 4x09 - Once La'An a Time [WEBDL-1080p]"

        assertEquals(body, plainText(body))
        assertEquals("", firstLink(body))
    }

    @Test
    fun `several links are all found, the first one wins`() {
        val source = "[one](https://a.example) then [two](https://b.example)"

        assertEquals("https://a.example", firstLink(source))
        assertEquals("one then two", plainText(source))
    }

    @Test
    fun `the empty body asks for nothing`() {
        assertEquals(emptyList<Span>(), parseInline(""))
        assertEquals("", firstLink(""))
    }
}
