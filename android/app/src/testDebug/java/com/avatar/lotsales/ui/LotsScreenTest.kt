package com.avatar.lotsales.ui

import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.hasScrollToIndexAction
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.test.performScrollToNode
import androidx.test.ext.junit.runners.AndroidJUnit4
import com.avatar.lotsales.data.LotRepository
import com.avatar.lotsales.data.column
import com.avatar.lotsales.data.db.AppDatabase
import com.avatar.lotsales.data.db.BlockedLotEntity
import com.avatar.lotsales.data.db.ItemEntity
import com.avatar.lotsales.data.db.LotEntity
import com.avatar.lotsales.data.inMemoryDb
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import java.math.BigDecimal

/**
 * Lotlar: ilk ekran yalnız özet + liste butonu; listede satıra dokununca ayrıntılar açılır. Bloke lotun
 * gerekçesi görünür ve satış butonu yoktur; aktif lotun butonu o lotla satışı açar.
 */
@RunWith(AndroidJUnit4::class)
class LotsScreenTest {
    @get:Rule
    val compose = createComposeRule()

    private lateinit var db: AppDatabase

    // aktif lotun SKT'si test tarihine göre kritik olabilir: buton metni ikisinden biri
    private val sellButton = hasText("Satışa ver") or hasText("Öncelikli sat")

    @Before
    fun setUp() = runBlocking {
        db = inMemoryDb()
        db.master().upsertItem(ItemEntity("i1", "SUCUK", "Narin Sucuk 1 kg", "finished", "adet", BigDecimal("1.000"), BigDecimal("24.9000")))
        db.master().insertLot(LotEntity("a", "SUC-2026-045", "i1", "active", "2026-12-15", BigDecimal("18.1588"), BigDecimal("85.000"), 1))
        db.master().insertLot(LotEntity("b", "SUC-2026-099", "i1", "blocked", "2026-11-30", BigDecimal("18.2000"), BigDecimal("40.000"), 2))
        db.master().insertBlocked(listOf(BlockedLotEntity("b", "geri cagirma", "2026-10-26T10:00:00Z")))
        check(db.column("SELECT id FROM lots").size == 2)
    }

    @After
    fun tearDown() = db.close()

    @Test
    fun summaryShowsNoLotRowsAndButtonOpensList() {
        var opened = false
        val vm = LotsViewModel(LotRepository(db))
        compose.setContent { LotsScreen(onOpenList = { opened = true }, vm = vm) }

        // Room Flow'u kendi executor'unda calisir; compose kurali onu beklemez.
        compose.waitUntil(5_000) { vm.all.value.size == 2 }
        compose.onNodeWithText("SUC-2026-045", substring = true).assertDoesNotExist()
        compose.onNodeWithText("Lot listesini aç", substring = true).performScrollTo().performClick()

        assertTrue(opened)
    }

    @Test
    fun tappingRowShowsDetailsAndOnlyActiveLotCanBeSold() {
        val opened = mutableListOf<String>()
        val vm = LotsViewModel(LotRepository(db))
        compose.setContent { LotListScreen(onSell = { opened += it }, vm = vm, warehouse = "WH-MAIN") }

        compose.waitUntil(5_000) { vm.lots.value.size == 2 }
        val list = compose.onNode(hasScrollToIndexAction())

        // kapalı satırda ayrıntı yok; bloke satır dokununca gerekçe açılır, satış butonu çıkmaz
        compose.onNodeWithText("geri cagirma").assertDoesNotExist()
        compose.onNodeWithText("SUC-2026-099", substring = true).performClick()
        list.performScrollToNode(hasText("geri cagirma"))
        compose.onAllNodes(sellButton).assertCountEquals(0)

        // aktif satır (FEFO: bloke lottan sonra) açılınca bloke satır kapanır; depo ve satış butonu görünür
        list.performScrollToNode(hasText("SUC-2026-045", substring = true))
        compose.onNodeWithText("SUC-2026-045", substring = true).performClick()
        compose.onNodeWithText("geri cagirma").assertDoesNotExist()
        list.performScrollToNode(sellButton)
        compose.onNodeWithText("WH-MAIN").assertExists()
        compose.onNode(sellButton).performClick()

        assertEquals(listOf("a"), opened)
    }

    @Test
    fun blockedChipListsOnlyBlockedLots() {
        val vm = LotsViewModel(LotRepository(db))
        compose.setContent { LotListScreen(onSell = {}, vm = vm) }

        compose.waitUntil(5_000) { vm.lots.value.size == 2 }
        compose.onNodeWithText("Bloke · 1").performClick()
        compose.waitUntil(5_000) { vm.lots.value.size == 1 }

        compose.onAllNodesWithText("SUC-2026-045", substring = true).assertCountEquals(0)
        compose.onNodeWithText("SUC-2026-099", substring = true).assertExists()
    }
}
