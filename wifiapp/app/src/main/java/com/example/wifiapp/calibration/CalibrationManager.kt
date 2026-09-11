package com.example.wifiapp.calibration

import com.example.wifiapp.ar.CoordinateTransformer
import com.example.wifiapp.models.CalibrationState
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlin.math.atan2

class CalibrationManager(private val transformer: CoordinateTransformer) {

    private val _calibrationState = MutableStateFlow(CalibrationState())
    val calibrationState: StateFlow<CalibrationState> = _calibrationState.asStateFlow()

    /**
     * Calibrate the AR world at the user's current physical position and heading.
     *
     * @param serverX Server X coordinate (East, m) of this reference point (e.g. 0.0)
     * @param serverY Server Y coordinate (North, m) of this reference point (e.g. 0.0)
     * @param serverZ Server Z coordinate (Up, m) of this reference point (e.g. 0.0)
     * @param cameraArX Current ARCore camera X position (m)
     * @param cameraArY Current ARCore camera Y position (m)
     * @param cameraArZ Current ARCore camera Z position (m)
     * @param qx, qy, qz, qw Current ARCore camera orientation quaternion
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
        // Compute yaw angle around Y axis from quaternion
        // Camera looks along -Z in local space, forward vector rotated by quaternion:
        // forward = R * (0, 0, -1)
        val fwdX = -2f * (qx * qz + qw * qy)
        val fwdZ = -(1f - 2f * (qx * qx + qy * qy))
        val yawRad = atan2(fwdX.toDouble(), -fwdZ.toDouble())
        val yawDeg = Math.toDegrees(yawRad).toFloat()

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
            calibratedAtMs = System.currentTimeMillis()
        )

        _calibrationState.value = newState
        transformer.updateCalibration(newState)
    }

    fun resetCalibration() {
        val resetState = CalibrationState()
        _calibrationState.value = resetState
        transformer.reset()
    }
}
