package com.example.wifiapp.identity

import android.content.Context
import java.security.SecureRandom

object IdentityManager {
    private const val PREFS_NAME = "wifi_hunter_identity"
    private const val KEY_DEVICE_ID = "device_id"

    @Synchronized
    fun getDeviceId(context: Context): String {
        val prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
        var deviceId = prefs.getString(KEY_DEVICE_ID, null)
        if (deviceId == null) {
            val random = SecureRandom()
            val bytes = ByteArray(3)
            random.nextBytes(bytes)
            val suffix = bytes.joinToString("") { "%02X".format(it) }
            deviceId = "MOBILE-$suffix"
            prefs.edit().putString(KEY_DEVICE_ID, deviceId).apply()
        }
        return deviceId
    }
}
