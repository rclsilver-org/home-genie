package io.github.rclsilver.home_notifications

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.material3.MaterialTheme
import androidx.core.content.ContextCompat
import androidx.lifecycle.lifecycleScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import io.github.rclsilver.home_notifications.data.Settings
import io.github.rclsilver.home_notifications.ui.AppScreen
import io.github.rclsilver.home_notifications.ui.completeOidcLogin

class MainActivity : ComponentActivity() {

    private val requestNotifications =
        registerForActivityResult(ActivityResultContracts.RequestPermission()) { }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        // Without this permission the foreground service cannot show its
        // ongoing notification, and Android refuses to start it.
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS) !=
            PackageManager.PERMISSION_GRANTED
        ) {
            requestNotifications.launch(Manifest.permission.POST_NOTIFICATIONS)
        }

        val settings = Settings(applicationContext)
        setContent {
            MaterialTheme {
                AppScreen(settings = settings)
            }
        }

        // The application may have been launched *by* the redirect, if its
        // process had been killed during authentication.
        handleRedirect(intent)
    }

    // singleTask: the redirect lands here when the application is already
    // open, which is the common case when coming back from the Custom Tab.
    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        handleRedirect(intent)
    }

    private fun handleRedirect(intent: Intent?) {
        val data: Uri = intent?.data ?: return
        if (data.scheme != REDIRECT_SCHEME) return

        lifecycleScope.launch {
            loginError.value = completeOidcLogin(applicationContext, data)
        }
    }

    companion object {
        private const val REDIRECT_SCHEME = "io.github.rclsilver.home-notifications"

        private val loginError = MutableStateFlow<String?>(null)

        /** Last OIDC authentication error, shown by the screen. */
        val observedLoginError: StateFlow<String?> = loginError.asStateFlow()

        fun clearLoginError() {
            loginError.value = null
        }
    }
}
