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

/** Marka: ikondaki yesil (#2E7D32), sicak krem yuzeyler, amber vurgu (SKT/FEFO). Yalniz acik tema. */
private val LightColors = lightColorScheme(
    primary = Color(0xFF2E7D32),
    onPrimary = Color.White,
    primaryContainer = Color(0xFFC8EBC2),
    onPrimaryContainer = Color(0xFF0B3D10),
    inversePrimary = Color(0xFF8FD68B),
    secondary = Color(0xFF55624F),
    onSecondary = Color.White,
    secondaryContainer = Color(0xFFDCE8D2),
    onSecondaryContainer = Color(0xFF131F10),
    tertiary = Color(0xFF8C5A00),
    onTertiary = Color.White,
    tertiaryContainer = Color(0xFFFFDDB0),
    onTertiaryContainer = Color(0xFF2C1800),
    background = Color(0xFFFBF8F1),
    onBackground = Color(0xFF1C1C17),
    surface = Color(0xFFFBF8F1),
    onSurface = Color(0xFF1C1C17),
    surfaceVariant = Color(0xFFE3E3D6),
    onSurfaceVariant = Color(0xFF46483F),
    surfaceTint = Color(0xFF2E7D32),
    inverseSurface = Color(0xFF31312B),
    inverseOnSurface = Color(0xFFF3F1E8),
    error = Color(0xFFBA1A1A),
    onError = Color.White,
    errorContainer = Color(0xFFFFDAD6),
    onErrorContainer = Color(0xFF410002),
    outline = Color(0xFF77786D),
    outlineVariant = Color(0xFFC7C8BA),
    surfaceBright = Color(0xFFFBF8F1),
    surfaceDim = Color(0xFFDCD9D1),
    surfaceContainerLowest = Color.White,
    surfaceContainerLow = Color(0xFFF6F3EB),
    surfaceContainer = Color(0xFFF0EDE5),
    surfaceContainerHigh = Color(0xFFEAE7DF),
    surfaceContainerHighest = Color(0xFFE4E2DA),
)

/** Durum rozetleri; M3 semasinda karsiligi yok. Metin olarak kendi %12 tonlu zemininde (beyaz/krem/pembe kart ustu) >= 4.8:1. */
object StatusColors {
    val success = Color(0xFF236B28)
    val warning = Color(0xFF8A5000)
    val neutral = Color(0xFF5C5D55)
    val danger = Color(0xFFB3191A)
    val dangerContainer = Color(0xFFFFEDEA)
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
