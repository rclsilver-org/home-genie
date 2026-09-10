package io.github.rclsilver.home_notifications.net

import android.net.Uri
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import okhttp3.FormBody
import okhttp3.Request
import java.io.IOException

/** What OIDC discovery tells us. */
@Serializable
data class OidcEndpoints(
    @SerialName("authorization_endpoint") val authorization: String,
    @SerialName("token_endpoint") val token: String,
)

/** The configuration the server advertises. */
@Serializable
data class AuthConfig(val oidc: OidcSettings = OidcSettings())

@Serializable
data class OidcSettings(
    val enabled: Boolean = false,
    val issuer: String = "",
    @SerialName("client_id") val clientId: String = "",
)

@Serializable
private data class TokenResponse(
    @SerialName("id_token") val idToken: String = "",
    @SerialName("access_token") val accessToken: String = "",
)

/**
 * Redirect URI, to be kept identical to the OIDC client and the manifest.
 *
 * The scheme carries a **hyphen** where the applicationId carries an
 * underscore, and that is not a typo: an underscore is legal in a Java
 * package, but RFC 3986 forbids it in a URI scheme, and a browser may
 * refuse to redirect to it. Do not "fix" the asymmetry.
 */
const val REDIRECT_URI = "io.github.rclsilver.home-notifications://oauth2redirect"

private val json = Json { ignoreUnknownKeys = true }

/**
 * Asks the server how to authenticate rather than hard-coding the issuer:
 * changing realm or renaming the client then imposes no new release.
 */
suspend fun fetchAuthConfig(serverUrl: String): Result<AuthConfig> =
    withContext(Dispatchers.IO) {
        runCatching {
            val call = ApiClient.defaultClient().newCall(
                Request.Builder().url("${serverUrl.trimEnd('/')}/api/v1/auth/config").build()
            )
            call.execute().use { response ->
                val text = response.body?.string().orEmpty()
                if (!response.isSuccessful) throw IOException("HTTP error ${response.code}")
                json.decodeFromString(AuthConfig.serializer(), text)
            }
        }
    }

/** Discovers the identity provider's endpoints. */
suspend fun discover(issuer: String): Result<OidcEndpoints> =
    withContext(Dispatchers.IO) {
        runCatching {
            val url = "${issuer.trimEnd('/')}/.well-known/openid-configuration"
            ApiClient.defaultClient().newCall(Request.Builder().url(url).build())
                .execute().use { response ->
                    val text = response.body?.string().orEmpty()
                    if (!response.isSuccessful) throw IOException("HTTP error ${response.code}")
                    json.decodeFromString(OidcEndpoints.serializer(), text)
                }
        }
    }

/** Builds the authorization URL to open in the Custom Tab. */
fun authorizationUrl(
    endpoints: OidcEndpoints,
    clientId: String,
    pkce: Pkce,
    state: String,
): Uri = Uri.parse(endpoints.authorization).buildUpon()
    .appendQueryParameter("client_id", clientId)
    .appendQueryParameter("response_type", "code")
    .appendQueryParameter("redirect_uri", REDIRECT_URI)
    .appendQueryParameter("scope", "openid profile email")
    .appendQueryParameter("state", state)
    .appendQueryParameter("code_challenge", pkce.challenge)
    .appendQueryParameter("code_challenge_method", "S256")
    .build()

/**
 * Exchanges the code for the tokens. The verifier leaves here and only
 * here: it is what proves the code belongs to this request.
 */
suspend fun exchangeCode(
    endpoints: OidcEndpoints,
    clientId: String,
    code: String,
    verifier: String,
): Result<String> = withContext(Dispatchers.IO) {
    runCatching {
        val body = FormBody.Builder()
            .add("grant_type", "authorization_code")
            .add("client_id", clientId)
            .add("code", code)
            .add("redirect_uri", REDIRECT_URI)
            .add("code_verifier", verifier)
            .build()

        ApiClient.defaultClient()
            .newCall(Request.Builder().url(endpoints.token).post(body).build())
            .execute().use { response ->
                val text = response.body?.string().orEmpty()
                if (!response.isSuccessful) {
                    throw IOException("code exchange refused (HTTP ${response.code})")
                }
                val tokens = json.decodeFromString(TokenResponse.serializer(), text)
                if (tokens.idToken.isEmpty()) {
                    // Without the openid scope the provider returns no ID
                    // token — and that is the only one the server verifies.
                    throw IOException("no id_token in the response")
                }
                tokens.idToken
            }
    }
}

/** Exchanges the provider's ID token for a device token from the server. */
suspend fun loginWithIdToken(
    serverUrl: String,
    idToken: String,
    deviceName: String,
): Result<LoginResponse> = withContext(Dispatchers.IO) {
    runCatching {
        val payload = json.encodeToString(
            OidcLoginRequest.serializer(),
            OidcLoginRequest(idToken = idToken, deviceName = deviceName),
        )
        val call = ApiClient.defaultClient().newCall(
            Request.Builder()
                .url("${serverUrl.trimEnd('/')}/api/v1/auth/oidc")
                .post(payload.toRequestBodyJson())
                .build()
        )
        call.execute().use { response ->
            val text = response.body?.string().orEmpty()
            if (!response.isSuccessful) {
                val message = runCatching {
                    json.decodeFromString(ErrorResponse.serializer(), text).error
                }.getOrElse { "HTTP error ${response.code}" }
                throw IOException(message)
            }
            json.decodeFromString(LoginResponse.serializer(), text)
        }
    }
}

@Serializable
data class OidcLoginRequest(
    @SerialName("id_token") val idToken: String,
    @SerialName("device_name") val deviceName: String,
    val platform: String = "android",
)
