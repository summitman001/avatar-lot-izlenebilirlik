package com.avatar.lotsales.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.unit.dp
import com.avatar.lotsales.R
import java.time.LocalDate
import java.time.format.DateTimeFormatter
import java.time.temporal.ChronoUnit
import java.util.Locale

const val EXPIRY_CRITICAL_DAYS = 7
const val EXPIRY_SOON_DAYS = 30

/** Sıra önemli: EXPIRED..SOON "SKT yakın" sayılır. */
enum class ExpiryLevel { EXPIRED, CRITICAL, SOON, OK, NONE }

data class Expiry(val level: ExpiryLevel, val days: Long? = null, val date: LocalDate? = null) {
    val urgent: Boolean get() = level <= ExpiryLevel.SOON
}

/** SKT (YYYY-MM-DD) aciliyeti; SKT günü dahil satılabilir sayılır (0 gün = bugün). Tarih yok/bozuksa NONE. */
fun expiryOf(expiryDate: String?, today: LocalDate = LocalDate.now()): Expiry {
    val date = expiryDate?.let { runCatching { LocalDate.parse(it) }.getOrNull() } ?: return Expiry(ExpiryLevel.NONE)
    val days = ChronoUnit.DAYS.between(today, date)
    val level = when {
        days < 0 -> ExpiryLevel.EXPIRED
        days <= EXPIRY_CRITICAL_DAYS -> ExpiryLevel.CRITICAL
        days <= EXPIRY_SOON_DAYS -> ExpiryLevel.SOON
        else -> ExpiryLevel.OK
    }
    return Expiry(level, days, date)
}

private val dateFormat = DateTimeFormatter.ofPattern("dd.MM.yyyy")

@Composable
fun ExpiryBadge(expiryDate: String?) {
    val e = remember(expiryDate) { expiryOf(expiryDate) }
    val (text, color) = when (e.level) {
        ExpiryLevel.EXPIRED -> stringResource(R.string.expiry_expired) to StatusColors.danger
        ExpiryLevel.CRITICAL ->
            (if (e.days == 0L) stringResource(R.string.expiry_today) else stringResource(R.string.expiry_days, e.days!!)) to StatusColors.danger
        ExpiryLevel.SOON -> stringResource(R.string.expiry_days, e.days!!) to StatusColors.warning
        ExpiryLevel.OK -> stringResource(R.string.expiry_date, dateFormat.format(e.date)) to StatusColors.success
        ExpiryLevel.NONE -> stringResource(R.string.expiry_none) to StatusColors.neutral
    }
    Chip(text, color, R.drawable.ic_schedule)
}

private val tr = Locale.forLanguageTag("tr")

/** "Narin Sucuk 1 kg" -> "NS" (Türkçe büyük harf: i -> İ). */
fun initials(name: String): String =
    name.split(' ', '-').filter { it.firstOrNull()?.isLetter() == true }.take(2)
        .joinToString("") { it.first().toString() }.uppercase(tr).ifEmpty { "?" }

private val avatarTones = listOf(
    Color(0xFFC8EBC2) to Color(0xFF0B3D10), // yeşil
    Color(0xFFFFDDB0) to Color(0xFF4A2800), // amber
    Color(0xFFD7E3FF) to Color(0xFF0F2D5C), // mavi
    Color(0xFFF7D8E6) to Color(0xFF4F1631), // gül
    Color(0xFFE2DCF7) to Color(0xFF2B2152), // lavanta
    Color(0xFFCDEBEA) to Color(0xFF0B3B39), // camgöbeği
)

/** Ürün/müşteri adının baş harfleri, isme göre sabit renkte daire. Görsel süs: ekran okuyucudan gizli. */
@Composable
fun InitialsAvatar(name: String, modifier: Modifier = Modifier) {
    val (bg, fg) = avatarTones[(name.hashCode() and Int.MAX_VALUE) % avatarTones.size]
    Box(
        modifier.size(44.dp).background(bg, CircleShape).clearAndSetSemantics {},
        contentAlignment = Alignment.Center,
    ) {
        Text(initials(name), color = fg, style = MaterialTheme.typography.titleSmall)
    }
}
