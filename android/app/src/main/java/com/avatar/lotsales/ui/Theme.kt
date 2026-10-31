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
 * Marka: ikondaki yesil (#2E7D32). Yuzeyler soguk, hafif yesil tonlu notr (web paneliyle ayni aile; Stitch
 * tasarimindaki katmanli yuzey dili). tertiary = kritik SKT turuncusu (Stitch). Yalniz acik tema.
 */
private val LightColors = lightColorScheme(
    primary = Color(0xFF2E7D32),
    onPrimary = Color.White,
    primaryContainer = Color(0xFFC8EBC2),
    onPrimaryContainer = Color(0xFF0B3D10),
    inversePrimary = Color(0xFF8FD68B),
    secondary = Color(0xFF52634F),
    onSecondary = Color.White,
    secondaryContainer = Color(0xFFD5E8CF),
    onSecondaryContainer = Color(0xFF101F0F),
    tertiary = Color(0xFF9A4600),
    onTertiary = Color.White,
    tertiaryContainer = Color(0xFFFFDBC9),
    onTertiaryContainer = Color(0xFF321200),
    background = Color(0xFFF5F8F4),
    onBackground = Color(0xFF191D18),
    surface = Color(0xFFF5F8F4),
    onSurface = Color(0xFF191D18),
    surfaceVariant = Color(0xFFDDE5D9),
    onSurfaceVariant = Color(0xFF424940),
    surfaceTint = Color(0xFF2E7D32),
    inverseSurface = Color(0xFF2E322D),
    inverseOnSurface = Color(0xFFEFF2EC),
    error = Color(0xFFBA1A1A),
    onError = Color.White,
    errorContainer = Color(0xFFFFDAD6),
    onErrorContainer = Color(0xFF93000A),
    outline = Color(0xFF72796F),
    outlineVariant = Color(0xFFC2C9BE),
    surfaceBright = Color(0xFFF5F8F4),
    surfaceDim = Color(0xFFD6DBD4),
    surfaceContainerLowest = Color.White,
    surfaceContainerLow = Color(0xFFEFF3EE),
    surfaceContainer = Color(0xFFE9EEE8),
    surfaceContainerHigh = Color(0xFFE3E9E2),
    surfaceContainerHighest = Color(0xFFDDE3DC),
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
