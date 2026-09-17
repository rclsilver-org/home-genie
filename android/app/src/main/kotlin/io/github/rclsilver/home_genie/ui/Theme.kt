package io.github.rclsilver.home_genie.ui

import android.app.Activity
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.SideEffect
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalView
import androidx.core.view.WindowCompat

/**
 * What the application should follow: the system, or an explicit choice.
 *
 * Stored on the device rather than on the server. Which theme suits depends
 * on the screen one is holding and the light in the room, not on the account
 * — a phone in bed at three in the morning and a tablet in daylight want
 * different answers from the same user.
 */
enum class ThemeChoice(val label: String) {
    SYSTEM("System"),
    LIGHT("Light"),
    DARK("Dark");

    companion object {
        fun of(stored: String): ThemeChoice =
            entries.firstOrNull { it.name == stored } ?: SYSTEM
    }
}

/**
 * The palette is fixed rather than drawn from the wallpaper.
 *
 * Material You would repaint `error` with whatever hue the user's background
 * happens to suggest, and on this screen red is not decoration: it is the
 * difference between an alert nobody has taken and one that is handled.
 * A console whose severity colours move with the wallpaper cannot be read at
 * a glance, which is the only thing it is for.
 *
 * The roles carry meaning here, so they are chosen for it:
 *   error      an open alert nobody has taken
 *   tertiary   the warning severity
 *   secondary  an alert someone has taken
 *   outline    borders and neutral badges, never a signal of its own
 */
private val DARK = darkColorScheme(
    primary = Color(0xFF5B9DF9),
    onPrimary = Color(0xFF07131F),
    primaryContainer = Color(0xFF123156),
    onPrimaryContainer = Color(0xFFCFE1FF),
    secondary = Color(0xFF9AA5B4),
    onSecondary = Color(0xFF0E1116),
    tertiary = Color(0xFFF0A83C),
    onTertiary = Color(0xFF1A1200),
    background = Color(0xFF0E1116),
    onBackground = Color(0xFFE6EAF0),
    surface = Color(0xFF151A21),
    onSurface = Color(0xFFE6EAF0),
    surfaceVariant = Color(0xFF1E252F),
    onSurfaceVariant = Color(0xFFA8B2C1),
    outline = Color(0xFF3A4553),
    error = Color(0xFFE5484D),
    onError = Color(0xFFFFFFFF),
    // The fill of a row that still demands something. Dark enough that a
    // screenful of them is still readable — a list of bright red cards
    // signals nothing, because everything shouts equally.
    errorContainer = Color(0xFF2E161A),
    onErrorContainer = Color(0xFFFFC9CC),
)

private val LIGHT = lightColorScheme(
    primary = Color(0xFF1D62D6),
    onPrimary = Color(0xFFFFFFFF),
    primaryContainer = Color(0xFFD9E5FF),
    onPrimaryContainer = Color(0xFF082352),
    secondary = Color(0xFF5A6675),
    onSecondary = Color(0xFFFFFFFF),
    tertiary = Color(0xFFA96500),
    onTertiary = Color(0xFFFFFFFF),
    background = Color(0xFFF7F8FA),
    onBackground = Color(0xFF10151C),
    surface = Color(0xFFFFFFFF),
    onSurface = Color(0xFF10151C),
    surfaceVariant = Color(0xFFEDEFF3),
    onSurfaceVariant = Color(0xFF4A5462),
    outline = Color(0xFFC3CBD6),
    error = Color(0xFFC62828),
    onError = Color(0xFFFFFFFF),
    errorContainer = Color(0xFFFDEBEB),
    onErrorContainer = Color(0xFF5F1414),
)

@Composable
fun HomeGenieTheme(choice: ThemeChoice, content: @Composable () -> Unit) {
    val dark = when (choice) {
        ThemeChoice.SYSTEM -> isSystemInDarkTheme()
        ThemeChoice.LIGHT -> false
        ThemeChoice.DARK -> true
    }
    // The system bars draw their icons from the system's theme, not ours. An
    // explicit Light chosen on a phone set to dark left the clock and the
    // battery painted light on a light background — invisible, and only
    // visible as a bug once the two disagreed.
    val view = LocalView.current
    if (!view.isInEditMode) {
        SideEffect {
            val window = (view.context as Activity).window
            WindowCompat.getInsetsController(window, view).apply {
                isAppearanceLightStatusBars = !dark
                isAppearanceLightNavigationBars = !dark
            }
        }
    }

    MaterialTheme(colorScheme = if (dark) DARK else LIGHT, content = content)
}
