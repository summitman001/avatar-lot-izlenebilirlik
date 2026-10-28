package com.avatar.lotsales.data

import android.content.Context
import androidx.room.Room
import androidx.sqlite.db.SupportSQLiteDatabase
import androidx.sqlite.db.SupportSQLiteOpenHelper
import androidx.sqlite.db.framework.FrameworkSQLiteOpenHelperFactory
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import com.avatar.lotsales.data.db.AppDatabase
import com.avatar.lotsales.data.db.SyncKeys
import com.avatar.lotsales.data.net.AppJson
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import java.io.File
import java.math.BigDecimal

/** v1 kurulu bir cihaz v2'ye yukselince: tablolar eklenir, imlec ve bakiyeler sifirlanir (depolar yeniden gelir). */
@RunWith(AndroidJUnit4::class)
class MigrationTest {
    @Test
    fun v1DatabaseUpgradesAndResetsPullCursorAndBalances() = runBlocking {
        val context = ApplicationProvider.getApplicationContext<Context>()
        val name = "migration-test.db"
        context.deleteDatabase(name)
        createV1(context, name) { db ->
            db.execSQL("INSERT INTO sync_state (`key`, value) VALUES ('last_pull_seq', '8')")
            db.execSQL(
                """INSERT INTO lots (id, lot_no, item_id, status, expiry_date, unit_cost, qty_on_hand_server, updated_seq)
                   VALUES ('l1', 'SUC-2026-045', 'i1', 'active', '2026-12-15', '18.1588', '85.000', 6)""",
            )
        }

        val db = Room.databaseBuilder(context, AppDatabase::class.java, name).allowMainThreadQueries().build()
        assertEquals("0", db.sync().state(SyncKeys.LAST_PULL_SEQ))
        assertEquals(BigDecimal("0.000"), db.master().lot("l1")!!.qtyOnHandServer)
        assertEquals(0, db.count("warehouses") + db.count("customer_returns")) // v2 tablolari var
        db.close()
    }

    /** Disa aktarilmis v1 semasindan (app/schemas/.../1.json) gercek bir v1 veritabani kurar. */
    private fun createV1(context: Context, name: String, seed: (SupportSQLiteDatabase) -> Unit) {
        val schema = AppJson.parseToJsonElement(File("schemas/com.avatar.lotsales.data.db.AppDatabase/1.json").readText())
            .jsonObject.getValue("database").jsonObject
        val callback = object : SupportSQLiteOpenHelper.Callback(1) {
            override fun onCreate(db: SupportSQLiteDatabase) {
                schema.getValue("entities").jsonArray.map { it.jsonObject }.forEach { entity ->
                    val table = entity.getValue("tableName").jsonPrimitive.content
                    val sqls = listOf(entity.getValue("createSql")) + entity["indices"]?.jsonArray.orEmpty().map { it.jsonObject.getValue("createSql") }
                    sqls.forEach { db.execSQL(it.jsonPrimitive.content.replace("\${TABLE_NAME}", table)) }
                }
                schema.getValue("setupQueries").jsonArray.forEach { db.execSQL(it.jsonPrimitive.content) }
            }

            override fun onUpgrade(db: SupportSQLiteDatabase, oldVersion: Int, newVersion: Int) = Unit
        }
        val helper = FrameworkSQLiteOpenHelperFactory().create(
            SupportSQLiteOpenHelper.Configuration.builder(context).name(name).callback(callback).build(),
        )
        seed(helper.writableDatabase)
        helper.close()
    }
}
