package io.github.rclsilver.home_genie.net

import android.util.Base64
import java.security.MessageDigest
import java.security.SecureRandom

/**
 * A PKCE verifier / challenge pair (RFC 7636).
 *
 * This is what replaces the client secret a native application cannot keep:
 * the challenge goes out with the authorization request, the verifier is only
 * revealed when the code is exchanged. An intercepted code is therefore
 * useless without the verifier, which never left the device.
 */
data class Pkce(val verifier: String, val challenge: String) {

    companion object {
        /** 32 bytes, that is 43 base64url characters — the RFC's minimum. */
        private const val VERIFIER_BYTES = 32

        fun generate(): Pkce {
            val raw = ByteArray(VERIFIER_BYTES).also { SecureRandom().nextBytes(it) }
            val verifier = encode(raw)
            val digest = MessageDigest.getInstance("SHA-256").digest(verifier.toByteArray())
            return Pkce(verifier = verifier, challenge = encode(digest))
        }

        /** An anti-replay state, drawn the same way. */
        fun randomState(): String =
            encode(ByteArray(16).also { SecureRandom().nextBytes(it) })

        // base64url without padding, as the RFC requires.
        private fun encode(bytes: ByteArray): String =
            Base64.encodeToString(
                bytes,
                Base64.URL_SAFE or Base64.NO_PADDING or Base64.NO_WRAP,
            )
    }
}
