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
// Every role is named, none left to the default.
//
// Material fills whatever a scheme omits from its own baseline, which is a
// violet-brown family with nothing to do with these colours. Seventeen roles
// were unnamed, and they surfaced where components reach for them without
// being asked: a selected filter chip takes `secondaryContainer`, so the one
// answering "what is open" was painted a washed-out mauve.
private val DARK = darkColorScheme(
    primary = Color(0xFF5B9DF9),
    onPrimary = Color(0xFF07131F),
    primaryContainer = Color(0xFF123156),
    onPrimaryContainer = Color(0xFFCFE1FF),
    secondary = Color(0xFF9AA5B4),
    onSecondary = Color(0xFF0E1116),
    // What a selected chip is filled with: blue enough to read as chosen,
    // quiet enough not to compete with the primary it sits beside.
    secondaryContainer = Color(0xFF243447),
    onSecondaryContainer = Color(0xFFD4E2F5),
    tertiary = Color(0xFFF0A83C),
    onTertiary = Color(0xFF1A1200),
    tertiaryContainer = Color(0xFF3B2A0A),
    onTertiaryContainer = Color(0xFFFFDFA8),
    background = Color(0xFF0E1116),
    onBackground = Color(0xFFE6EAF0),
    surface = Color(0xFF151A21),
    onSurface = Color(0xFFE6EAF0),
    surfaceVariant = Color(0xFF1E252F),
    onSurfaceVariant = Color(0xFFA8B2C1),
    outline = Color(0xFF3A4553),
    outlineVariant = Color(0xFF262E38),
    scrim = Color(0xFF000000),
    surfaceTint = Color(0xFF5B9DF9),
    inversePrimary = Color(0xFF1D62D6),
    inverseSurface = Color(0xFFE6EAF0),
    inverseOnSurface = Color(0xFF151A21),
    surfaceDim = Color(0xFF0E1116),
    surfaceBright = Color(0xFF262E38),
    surfaceContainerLowest = Color(0xFF0B0E12),
    surfaceContainerLow = Color(0xFF131820),
    surfaceContainer = Color(0xFF171D25),
    surfaceContainerHigh = Color(0xFF1C232C),
    surfaceContainerHighest = Color(0xFF222A34),
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
    secondaryContainer = Color(0xFFDCE7F7),
    onSecondaryContainer = Color(0xFF16283E),
    tertiary = Color(0xFFA96500),
    onTertiary = Color(0xFFFFFFFF),
    tertiaryContainer = Color(0xFFFFE6C2),
    onTertiaryContainer = Color(0xFF3A2200),
    background = Color(0xFFF7F8FA),
    onBackground = Color(0xFF10151C),
    surface = Color(0xFFFFFFFF),
    onSurface = Color(0xFF10151C),
    surfaceVariant = Color(0xFFEDEFF3),
    onSurfaceVariant = Color(0xFF4A5462),
    outline = Color(0xFFC3CBD6),
    outlineVariant = Color(0xFFDFE4EA),
    scrim = Color(0xFF000000),
    surfaceTint = Color(0xFF1D62D6),
    inversePrimary = Color(0xFF5B9DF9),
    inverseSurface = Color(0xFF10151C),
    inverseOnSurface = Color(0xFFF7F8FA),
    surfaceDim = Color(0xFFE4E8EE),
    surfaceBright = Color(0xFFFFFFFF),
    surfaceContainerLowest = Color(0xFFFFFFFF),
    surfaceContainerLow = Color(0xFFF7F8FA),
    surfaceContainer = Color(0xFFF1F3F6),
    surfaceContainerHigh = Color(0xFFEBEEF2),
    surfaceContainerHighest = Color(0xFFE5E9EF),
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
