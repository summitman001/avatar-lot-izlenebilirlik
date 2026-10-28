package com.avatar.lotsales.ui

import androidx.compose.ui.test.assertHasClickAction
import androidx.compose.ui.test.assertHasNoClickAction
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
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
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import java.math.BigDecimal

/** Lot listesi: bloke lot kirmizi rozetli ve tiklanamaz; aktif lot tiklaninca o lotla satis acilir. */
@RunWith(AndroidJUnit4::class)
class LotsScreenTest {
    @get:Rule
    val compose = createComposeRule()

    private lateinit var db: AppDatabase

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
    fun blockedLotIsNotClickableAndActiveLotOpensSale() {
        val opened = mutableListOf<String>()
        compose.setContent { LotsScreen(onSell = { opened += it }, vm = LotsViewModel(LotRepository(db))) }

        // Room Flow'u kendi executor'unda calisir; compose kurali onu beklemez
        compose.waitUntil(5_000) { compose.onAllNodesWithText("BLOCKED").fetchSemanticsNodes().isNotEmpty() }
        compose.onNodeWithText("SUC-2026-099", substring = true).assertHasNoClickAction()
        compose.onNodeWithText("SUC-2026-045", substring = true).assertHasClickAction().performClick()

        assertEquals(listOf("a"), opened)
    }
}
