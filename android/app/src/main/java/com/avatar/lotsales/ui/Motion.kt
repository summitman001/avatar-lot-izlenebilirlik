@file:OptIn(ExperimentalSharedTransitionApi::class)

package com.avatar.lotsales.ui

import androidx.annotation.DrawableRes
import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.AnimatedVisibilityScope
import androidx.compose.animation.BoundsTransform
import androidx.compose.animation.ExperimentalSharedTransitionApi
import androidx.compose.animation.SharedTransitionScope
import androidx.compose.animation.SizeTransform
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.Spring
import androidx.compose.animation.core.VisibilityThreshold
import androidx.compose.animation.core.animateDpAsState
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.spring
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.scaleIn
import androidx.compose.animation.scaleOut
import androidx.compose.animation.slideInVertically
import androidx.compose.animation.slideOutVertically
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsPressedAsState
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.LocalTextStyle
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.PathMeasure
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.text
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay

// "Canli ama hizli": yaylar belirgin ama ~250-400 ms'de oturur. Sistemin animasyon olcegine Compose kendisi uyar.
fun <T> bouncy() = spring<T>(dampingRatio = 0.6f, stiffness = Spring.StiffnessMediumLow)
fun <T> settle() = spring<T>(dampingRatio = 0.85f, stiffness = Spring.StiffnessMediumLow)

/** NavHost'u saran SharedTransitionLayout ve o anki hedefin animasyon kapsami; NavHost disinda (testler) null. */
val LocalSharedScope = staticCompositionLocalOf<SharedTransitionScope?> { null }
val LocalNavScope = staticCompositionLocalOf<AnimatedVisibilityScope?> { null }

/** Ekranlar arasi ayni key'li kart bir digerine donusur (lot karti -> satis, satis karti -> detay). Kapsam yoksa etkisiz. */
@Composable
fun Modifier.sharedCard(key: String, shape: Shape): Modifier {
    val shared = LocalSharedScope.current ?: return this
    val nav = LocalNavScope.current ?: return this
    return with(shared) {
        this@sharedCard.sharedBounds(
            rememberSharedContentState(key),
            nav,
            boundsTransform = BoundsTransform { _, _ -> spring(0.8f, Spring.StiffnessMediumLow, Rect.VisibilityThreshold) },
            resizeMode = SharedTransitionScope.ResizeMode.RemeasureToBounds,
            clipInOverlayDuringTransition = OverlayClip(shape),
        )
    }
}

/**
 * Liste ilk dolduğunda kartlar sırayla (30 ms arayla, en fazla 8) aşağıdan belirir.
 * enabled=false iken (kaydırınca gelen kartlar, sonraki güncellemeler) doğrudan görünür.
 */
@Composable
fun Modifier.staggeredEnter(index: Int, enabled: Boolean): Modifier {
    val progress = remember { Animatable(if (enabled) 0f else 1f) }
    LaunchedEffect(Unit) {
        if (progress.value < 1f) {
            delay(30L * index.coerceAtMost(8))
            progress.animateTo(1f, settle())
        }
    }
    return graphicsLayer {
        alpha = progress.value.coerceIn(0f, 1f)
        translationY = (1f - progress.value) * 40.dp.toPx()
    }
}

/** Liste ilk kez veriyle dolana kadar true; staggeredEnter'a verilir (ekrana dönüşte tekrar oynamaz). */
@Composable
fun rememberFirstFill(hasData: Boolean): Boolean {
    var waiting by rememberSaveable { mutableStateOf(true) }
    LaunchedEffect(hasData) { if (hasData) waiting = false }
    return waiting
}

/** trigger her değiştiğinde (null hariç) yatay sallanır ve "red" titreşimi verir. */
@Composable
fun Modifier.shake(trigger: Any?): Modifier {
    val offset = remember { Animatable(0f) }
    val haptic = LocalHapticFeedback.current
    LaunchedEffect(trigger) {
        if (trigger == null) return@LaunchedEffect
        haptic.performHapticFeedback(HapticFeedbackType.Reject)
        for (x in listOf(12f, -10f, 7f, -4f, 0f)) offset.animateTo(x, tween(45))
    }
    return graphicsLayer { translationX = offset.value.dp.toPx() }
}

/** Sürekli döner (senkron sürerken). */
@Composable
fun Modifier.spin(): Modifier {
    val angle by rememberInfiniteTransition("spin").animateFloat(0f, 360f, infiniteRepeatable(tween(900, easing = LinearEasing)), "angle")
    return graphicsLayer { rotationZ = angle }
}

/**
 * Odometre: değişen her hane artışta aşağıdan, azalışta yukarıdan kayar. Haneler sağdan hizalı eşlenir;
 * metin her an tam değerdir (BigDecimal string), ara değer/yuvarlama yok.
 */
@Composable
fun RollingText(text: String, modifier: Modifier = Modifier, style: TextStyle = LocalTextStyle.current, color: Color = Color.Unspecified) {
    val numeric = style.merge(TextStyle(fontFeatureSettings = "tnum"))
    Row(modifier.clearAndSetSemantics { this.text = AnnotatedString(text) }) {
        text.forEachIndexed { i, ch ->
            key(text.length - i) {
                AnimatedContent(
                    ch,
                    transitionSpec = {
                        val up = targetState > initialState
                        (slideInVertically(bouncy()) { if (up) it else -it } + fadeIn()) togetherWith
                            (slideOutVertically { if (up) -it else it } + fadeOut()) using SizeTransform(clip = false)
                    },
                    label = "digit",
                ) { c -> Text(c.toString(), style = numeric, color = color) }
            }
        }
    }
}

/** Ana eylem butonu: basınca hafif küçülür ve köşeleri yuvarlaktan karemsiye döner (M3 Expressive basma hissi). */
@Composable
fun BouncyButton(
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    tonal: Boolean = false,
    content: @Composable RowScope.() -> Unit,
) {
    val source = remember { MutableInteractionSource() }
    val pressed by source.collectIsPressedAsState()
    val scale by animateFloatAsState(if (pressed) 0.95f else 1f, bouncy(), label = "press")
    val corner by animateDpAsState(if (pressed) 12.dp else 28.dp, bouncy(), label = "corner")
    Button(
        onClick = onClick,
        modifier = modifier.heightIn(min = 52.dp).graphicsLayer { scaleX = scale; scaleY = scale },
        enabled = enabled,
        shape = RoundedCornerShape(corner),
        colors = if (tonal) ButtonDefaults.filledTonalButtonColors() else ButtonDefaults.buttonColors(),
        interactionSource = source,
        content = content,
    )
}

/** Yeni bir trigger geldiğinde ~1.2 sn true döner; aynı trigger'la ekrana dönüşte tekrar oynamaz. */
@Composable
fun rememberCelebration(trigger: Any?): Boolean {
    var celebrated by rememberSaveable { mutableStateOf<Any?>(null) }
    var showing by remember { mutableStateOf(false) }
    LaunchedEffect(trigger) {
        if (trigger != null && trigger != celebrated) {
            celebrated = trigger
            showing = true
            delay(1200)
            showing = false
        }
    }
    return showing
}

/** Başarı: yeşil daire yaylanarak büyür, içinde tik çizilir; "onay" titreşimi. */
@Composable
fun SuccessCheck(visible: Boolean, modifier: Modifier = Modifier, diameter: Dp = 112.dp) {
    val haptic = LocalHapticFeedback.current
    AnimatedVisibility(
        visible,
        modifier,
        enter = scaleIn(bouncy(), initialScale = 0.3f) + fadeIn(),
        exit = scaleOut(targetScale = 0.8f) + fadeOut(),
    ) {
        val draw = remember { Animatable(0f) }
        LaunchedEffect(Unit) {
            haptic.performHapticFeedback(HapticFeedbackType.Confirm)
            delay(120)
            draw.animateTo(1f, tween(320, easing = FastOutSlowInEasing))
        }
        val color = StatusColors.success
        Canvas(Modifier.size(diameter)) {
            drawCircle(color)
            val w = size.width
            val check = Path().apply {
                moveTo(w * 0.29f, w * 0.52f)
                lineTo(w * 0.44f, w * 0.66f)
                lineTo(w * 0.72f, w * 0.37f)
            }
            val measure = PathMeasure().apply { setPath(check, false) }
            val partial = Path().also { measure.getSegment(0f, measure.length * draw.value, it, true) }
            drawPath(partial, Color.White, style = Stroke(width = w * 0.08f, cap = StrokeCap.Round, join = StrokeJoin.Round))
        }
    }
}

/** Boş liste: tonlu daire içinde hafifçe süzülen ikon + açıklama. */
@Composable
fun EmptyState(@DrawableRes icon: Int, text: String, modifier: Modifier = Modifier) {
    val y = rememberInfiniteTransition("float").animateFloat(-5f, 5f, infiniteRepeatable(tween(1600, easing = FastOutSlowInEasing), RepeatMode.Reverse), "y")
    Column(
        modifier.fillMaxWidth().padding(32.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Box(
            Modifier
                .graphicsLayer { translationY = y.value.dp.toPx() }
                .size(96.dp)
                .background(MaterialTheme.colorScheme.primaryContainer, CircleShape),
            contentAlignment = Alignment.Center,
        ) {
            Icon(painterResource(icon), null, Modifier.size(44.dp), tint = MaterialTheme.colorScheme.onPrimaryContainer)
        }
        Text(text, style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurfaceVariant, textAlign = TextAlign.Center)
    }
}
