package com.avatar.lotsales.di

import android.content.Context
import androidx.room.Room
import com.avatar.lotsales.BuildConfig
import com.avatar.lotsales.data.db.AppDatabase
import com.avatar.lotsales.data.net.AppJson
import com.avatar.lotsales.data.net.FakeApiInterceptor
import com.avatar.lotsales.data.net.SyncApi
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.logging.HttpLoggingInterceptor
import retrofit2.Retrofit
import retrofit2.converter.kotlinx.serialization.asConverterFactory
import retrofit2.create
import java.util.concurrent.TimeUnit
import javax.inject.Singleton

@Module
@InstallIn(SingletonComponent::class)
object AppModule {
    @Provides
    @Singleton
    fun database(@ApplicationContext context: Context): AppDatabase =
        Room.databaseBuilder(context, AppDatabase::class.java, "lotsales.db").build()

    @Provides
    @Singleton
    fun okHttp(@ApplicationContext context: Context): OkHttpClient = OkHttpClient.Builder()
        .apply {
            if (BuildConfig.DEBUG) addInterceptor(HttpLoggingInterceptor().setLevel(HttpLoggingInterceptor.Level.BODY))
            // logging'den sonra: sahte yanitlar da loglanir
            if (BuildConfig.USE_FAKE_API) {
                addInterceptor(FakeApiInterceptor { path -> context.assets.open(path).bufferedReader().use { it.readText() } })
            }
        }
        .connectTimeout(10, TimeUnit.SECONDS)
        .readTimeout(30, TimeUnit.SECONDS)
        .build()

    @Provides
    @Singleton
    fun syncApi(client: OkHttpClient): SyncApi = Retrofit.Builder()
        .baseUrl(BuildConfig.API_BASE_URL)
        .client(client)
        .addConverterFactory(AppJson.asConverterFactory("application/json; charset=utf-8".toMediaType()))
        .build()
        .create()
}
