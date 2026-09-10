package io.github.rclsilver.home_notifications.ui

import android.content.Context
import android.net.Uri
import android.util.Log
import androidx.browser.customtabs.CustomTabsIntent
import io.github.rclsilver.home_notifications.data.PendingLogin
import io.github.rclsilver.home_notifications.data.Settings
import io.github.rclsilver.home_notifications.net.Pkce
import io.github.rclsilver.home_notifications.net.authorizationUrl
import io.github.rclsilver.home_notifications.net.discover
import io.github.rclsilver.home_notifications.net.exchangeCode
import io.github.rclsilver.home_notifications.net.fetchAuthConfig
import io.github.rclsilver.home_notifications.net.loginWithIdToken
import io.github.rclsilver.home_notifications.service.ConnectionService

private const val TAG = "HomeGenie"

/**
 * Starts the Authorization Code + PKCE flow.
 *
 * The verifier and the state are persisted **before** opening the Custom
 * Tab: the application then goes to the background and Android may kill
 * its process while the user authenticates. Keeping them in memory would
 * make the code unusable on return.
 */
suspend fun startOidcLogin(context: Context, serverUrl: String): Result<Unit> = runCatching {
    val config = fetchAuthConfig(serverUrl).getOrThrow()
    if (!config.oidc.enabled) {
        error("this server has no identity provider configured")
    }

    val endpoints = discover(config.oidc.issuer).getOrThrow()
    val pkce = Pkce.generate()
    val state = Pkce.randomState()

    PendingLogin(context).save(
        verifier = pkce.verifier, state = state,
        issuer = config.oidc.issuer, clientId = config.oidc.clientId,
        serverUrl = serverUrl,
    )

    val url = authorizationUrl(endpoints, config.oidc.clientId, pkce, state)
    CustomTabsIntent.Builder().build().launchUrl(context, url)
}

/**
 * Handles the redirect from the identity provider.
 *
 * Returns an error message, or null on success. The call is idempotent: a
 * redirect with no pending flow is ignored, which happens when the system
 * reopens the application through the link.
 */
suspend fun completeOidcLogin(context: Context, redirect: Uri): String? {
    val pending = PendingLogin(context).read()
    if (!pending.isPresent) {
        Log.i(TAG, "redirect with no pending flow, ignored")
        return null
    }

    // The error returned by the provider is more telling than ours.
    redirect.getQueryParameter("error")?.let { error ->
        PendingLogin(context).clear()
        val description = redirect.getQueryParameter("error_description") ?: error
        return "the identity provider refused: $description"
    }

    // The state protects from replay and from a forged redirect: without
    // this check, any link opening our scheme could trigger an exchange.

    val state = redirect.getQueryParameter("state")
    if (state != pending.state) {
        PendingLogin(context).clear()
        return "unexpected state — redirect ignored"
    }

    val code = redirect.getQueryParameter("code")
        ?: run {
            PendingLogin(context).clear()
            return "no code in the redirect"
        }

    val endpoints = discover(pending.issuer).getOrElse {
        return "discovery failed: ${it.message}"
    }

    val idToken = exchangeCode(endpoints, pending.clientId, code, pending.verifier)
        .getOrElse {
            PendingLogin(context).clear()
            return "code exchange failed: ${it.message}"
        }

    // The verifier serves once; it is erased as soon as the exchange is done.
    PendingLogin(context).clear()

    val session = loginWithIdToken(pending.serverUrl, idToken, android.os.Build.MODEL ?: "android")
        .getOrElse { return "the server refused the token: ${it.message}" }

    Settings(context).saveSession(pending.serverUrl, session.token, session.user.username)
    ConnectionService.start(context)
    return null
}
