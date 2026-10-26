package com.avatar.lotsales.data

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import com.avatar.lotsales.data.db.AppDatabase

fun inMemoryDb(): AppDatabase =
    Room.inMemoryDatabaseBuilder(ApplicationProvider.getApplicationContext(), AppDatabase::class.java)
        .allowMainThreadQueries()
        .build()

fun AppDatabase.count(table: String): Int =
    openHelper.readableDatabase.query("SELECT COUNT(*) FROM $table").use { it.moveToFirst(); it.getInt(0) }

/** Ilk kolonu string liste olarak doner (testlerde ham satir kontrolu icin). */
fun AppDatabase.column(sql: String): List<String?> =
    openHelper.readableDatabase.query(sql).use { c -> buildList { while (c.moveToNext()) add(c.getString(0)) } }
