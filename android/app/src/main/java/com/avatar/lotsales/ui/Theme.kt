package com.avatar.lotsales.ui

import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Shapes
import androidx.compose.material3.Typography
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.Font
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontVariation
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.avatar.lotsales.R

/**
 * Marka: mavi/beyaz (#0B4FB3). Sayfa zemini soguk gri (#F3F5F9), kartlar beyaz; turuncu (tertiary = kritik SKT)
 * ve kirmizi (error = bloke) yalniz anlam tasidiginda. Yalniz acik tema.
 */
private val LightColors = lightColorScheme(
    primary = Color(0xFF0B4FB3),
    onPrimary = Color.White,
    primaryContainer = Color(0xFFE3ECFB),
    onPrimaryContainer = Color(0xFF0B4FB3),
    inversePrimary = Color(0xFFAFC6FF),
    secondary = Color(0xFF4A5A75),
    onSecondary = Color.White,
    secondaryContainer = Color(0xFFE8EDF5),
    onSecondaryContainer = Color(0xFF16233F),
    tertiary = Color(0xFF9A4A06),
    onTertiary = Color.White,
    tertiaryContainer = Color(0xFFFFF1E0),
    onTertiaryContainer = Color(0xFF7A3A04),
    background = Color(0xFFF3F5F9),
    onBackground = Color(0xFF16233F),
    surface = Color(0xFFF3F5F9),
    onSurface = Color(0xFF16233F),
    surfaceVariant = Color(0xFFE2E8F0),
    onSurfaceVariant = Color(0xFF4A5A75),
    surfaceTint = Color(0xFF0B4FB3),
    inverseSurface = Color(0xFF2A3550),
    inverseOnSurface = Color(0xFFEEF2F8),
    error = Color(0xFFB42318),
    onError = Color.White,
    errorContainer = Color(0xFFFDECEC),
    onErrorContainer = Color(0xFF8F1C13),
    outline = Color(0xFF7A879C),
    outlineVariant = Color(0xFFC9D3E3),
    surfaceBright = Color(0xFFF3F5F9),
    surfaceDim = Color(0xFFD9DFE9),
    surfaceContainerLowest = Color.White,
    surfaceContainerLow = Color(0xFFEEF2F8),
    surfaceContainer = Color(0xFFE8EDF5),
    surfaceContainerHigh = Color(0xFFE2E8F0),
    surfaceContainerHighest = Color(0xFFDCE3EE),
)

/** Durum rozetleri ve kart kenarlari; M3 semasinda karsiligi yok. Metin olarak kendi tonlu zemininde >= 4.8:1. */
object StatusColors {
    val success = Color(0xFF1E6B30)
    val successContainer = Color(0xFFE4F4E8)
    val warning = Color(0xFF8A5000)
    val neutral = Color(0xFF5B6B85)
    val danger = Color(0xFFB42318)
    val dangerContainer = Color(0xFFFDECEC)
    val dangerBorder = Color(0xFFF6C9C5)
    val criticalBorder = Color(0xFFFBD9B0)
    /** Beyaz kartin ince kenari */
    val cardBorder = Color(0xFFE2E8F0)
}

// Variable font: 'wght' ekseni acikca verilmezse her agirlik dosyanin varsayilan agirliginda cizilir (minSdk 26).
private val Jakarta = FontFamily(
    listOf(FontWeight.Normal, FontWeight.Medium, FontWeight.SemiBold, FontWeight.Bold, FontWeight.ExtraBold)
        .map { Font(R.font.plus_jakarta_sans, it, variationSettings = FontVariation.Settings(FontVariation.weight(it.weight))) },
)

private val AppTypography = Typography().run {
    fun TextStyle.j(weight: FontWeight? = null) = copy(fontFamily = Jakarta, fontWeight = weight ?: fontWeight)
    Typography(
        displayLarge = displayLarge.j(), displayMedium = displayMedium.j(), displaySmall = displaySmall.j(),
        headlineLarge = headlineLarge.j(FontWeight.Bold), headlineMedium = headlineMedium.j(FontWeight.Bold),
        headlineSmall = headlineSmall.j(FontWeight.Bold),
        titleLarge = titleLarge.j(FontWeight.Bold), titleMedium = titleMedium.j(FontWeight.SemiBold),
        titleSmall = titleSmall.j(FontWeight.SemiBold),
        bodyLarge = bodyLarge.j(), bodyMedium = bodyMedium.j(), bodySmall = bodySmall.j(),
        labelLarge = labelLarge.j(FontWeight.SemiBold), labelMedium = labelMedium.j(FontWeight.SemiBold),
        labelSmall = labelSmall.j(FontWeight.SemiBold),
    )
}

private val AppShapes = Shapes(
    extraSmall = RoundedCornerShape(8.dp),
    small = RoundedCornerShape(12.dp),
    medium = RoundedCornerShape(16.dp),
    large = RoundedCornerShape(24.dp),
    extraLarge = RoundedCornerShape(32.dp),
)

@Composable
fun LotSalesTheme(content: @Composable () -> Unit) =
    MaterialTheme(colorScheme = LightColors, shapes = AppShapes, typography = AppTypography, content = content)
