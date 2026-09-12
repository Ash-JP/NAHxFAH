package com.example.wifiapp.calibration

import android.content.Context
import com.example.wifiapp.ar.CoordinateTransformer
import com.example.wifiapp.models.CalibrationState
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlin.math.atan2

class CalibrationManager(
    private val context: Context,
    private val transformer: CoordinateTransformer
) {
    private val prefs = context.getSharedPreferences("wifi_hunter_calibration", Context.MODE_PRIVATE)

    private val _calibrationState = MutableStateFlow(loadSavedCalibration())
    val calibrationState: StateFlow<CalibrationState> = _calibrationState.asStateFlow()

    private fun loadSavedCalibration(): CalibrationState {
        val isCalibrated = prefs.getBoolean("is_calibrated", false)
        if (!isCalibrated) return CalibrationState()

        val anchorId = prefs.getString("anchor_id", "ORIGIN") ?: "ORIGIN"
        val serverX = prefs.getFloat("server_x", 0f).toDouble()
        val serverY = prefs.getFloat("server_y", 0f).toDouble()
        val serverZ = prefs.getFloat("server_z", 0f).toDouble()
        val arX = prefs.getFloat("ar_x", 0f)
        val arY = prefs.getFloat("ar_y", 0f)
        val arZ = prefs.getFloat("ar_z", 0f)
        val yaw = prefs.getFloat("yaw", 0f)
        val time = prefs.getLong("calibrated_at_ms", 0L)

        val restored = CalibrationState(
            isCalibrated = true,
            anchorId = anchorId,
            serverAnchorX = serverX,
            serverAnchorY = serverY,
            serverAnchorZ = serverZ,
            arOriginX = arX,
            arOriginY = arY,
            arOriginZ = arZ,
            rotationYawDegrees = yaw,
            calibratedAtMs = time
        )
        transformer.updateCalibration(restored)
        return restored
    }

    /**
     * Calibrate the AR world at the user's current physical position and heading.
     */
    fun setAnchorCalibration(
        anchorId: String = "ANCHOR-01",
        serverX: Double = 0.0,
        serverY: Double = 0.0,
        serverZ: Double = 0.0,
        cameraArX: Float,
        cameraArY: Float,
        cameraArZ: Float,
        qx: Float,
        qy: Float,
        qz: Float,
        qw: Float
    ) {
        val fwdX = -2f * (qx * qz + qw * qy)
        val fwdZ = -(1f - 2f * (qx * qx + qy * qy))
        val yawRad = atan2(fwdX.toDouble(), -fwdZ.toDouble())
        val yawDeg = Math.toDegrees(yawRad).toFloat()

        val now = System.currentTimeMillis()
        val newState = CalibrationState(
            isCalibrated = true,
            anchorId = anchorId,
            serverAnchorX = serverX,
            serverAnchorY = serverY,
            serverAnchorZ = serverZ,
            arOriginX = cameraArX,
            arOriginY = cameraArY,
            arOriginZ = cameraArZ,
            rotationYawDegrees = yawDeg,
            calibratedAtMs = now
        )

        // Persist to SharedPreferences so it survives app crashes
        prefs.edit()
            .putBoolean("is_calibrated", true)
            .putString("anchor_id", anchorId)
            .putFloat("server_x", serverX.toFloat())
            .putFloat("server_y", serverY.toFloat())
            .putFloat("server_z", serverZ.toFloat())
            .putFloat("ar_x", cameraArX)
            .putFloat("ar_y", cameraArY)
            .putFloat("ar_z", cameraArZ)
            .putFloat("yaw", yawDeg)
            .putLong("calibrated_at_ms", now)
            .apply()

        _calibrationState.value = newState
        transformer.updateCalibration(newState)
    }

    fun resetCalibration() {
        prefs.edit().clear().apply()
        val resetState = CalibrationState()
        _calibrationState.value = resetState
        transformer.reset()
    }
}
