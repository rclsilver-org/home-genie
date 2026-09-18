package io.github.rclsilver.home_genie.ui

/**
 * A piece of a message body, once the markers are understood.
 */
sealed interface Span {
    val text: String

    data class Plain(override val text: String) : Span
    data class Bold(override val text: String) : Span
    data class Link(override val text: String, val url: String) : Span
}

/**
 * Producers send Markdown whether or not anyone renders it.
 *
 * Diun writes `Docker tag [**image:tag**](https://…) which you subscribed
 * to…` and sets no click URL, so the address it wants followed is buried in
 * the text. Shown raw, the brackets and asterisks ate two lines of a feed and
 * the link went nowhere.
 *
 * Only the two constructs that actually arrive are understood — a link and
 * bold — and anything else is left as it was written. A half-implemented
 * Markdown that swallows an unmatched asterisk is worse than none: it loses
 * characters the producer meant to send.
 */
private val LINK = Regex("""\[([^\]]*)]\(([^)\s]+)\)""")
private val BOLD = Regex("""\*\*([^*]+)\*\*""")

/** The body, split into what it is made of. */
fun parseInline(source: String): List<Span> {
    val spans = mutableListOf<Span>()
    var index = 0
    val plain = StringBuilder()

    fun flush() {
        if (plain.isNotEmpty()) {
            spans.add(Span.Plain(plain.toString()))
            plain.clear()
        }
    }

    while (index < source.length) {
        val link = LINK.matchAt(source, index)
        if (link != null) {
            flush()
            // The label may be bold itself — `[**image**](url)` is what Diun
            // sends — and a link is already emphatic enough without it.
            spans.add(Span.Link(stripBold(link.groupValues[1]), link.groupValues[2]))
            index = link.range.last + 1
            continue
        }
        val bold = BOLD.matchAt(source, index)
        if (bold != null) {
            flush()
            spans.add(Span.Bold(bold.groupValues[1]))
            index = bold.range.last + 1
            continue
        }
        plain.append(source[index])
        index++
    }
    flush()
    return spans
}

/**
 * The same text with the markers gone, for the list, where two lines have to
 * carry the message and every bracket spent is a word lost.
 */
fun plainText(source: String): String =
    parseInline(source).joinToString("") { it.text }

/**
 * The first address the body points at, or "" — what the notification would
 * have put in its click URL had the producer filled one in.
 */
fun firstLink(source: String): String =
    parseInline(source).filterIsInstance<Span.Link>().firstOrNull()?.url ?: ""

private fun stripBold(text: String): String = BOLD.replace(text) { it.groupValues[1] }
