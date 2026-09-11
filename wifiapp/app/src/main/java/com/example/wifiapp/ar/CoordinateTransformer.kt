package com.example.wifiapp.ar

import com.example.wifiapp.models.APPosition
import com.example.wifiapp.models.CalibrationState
import kotlin.math.cos
import kotlin.math.sin

class CoordinateTransformer {
    private var calibration: CalibrationState = CalibrationState()

    fun updateCalibration(state: CalibrationState) {
        this.calibration = state
    }

    fun getCalibration(): CalibrationState = calibration

    fun isCalibrated(): Boolean = calibration.isCalibrated

    fun reset() {
        calibration = CalibrationState()
    }

    /**
     * Converts a 3D position in Server Local Coordinates (East=X, North=Y, Up=Z in meters)
     * into ARCore world coordinates (X=Right, Y=Up, Z=Back in meters).
     */
    fun serverToAr(serverPos: APPosition): Triple<Float, Float, Float> {
        if (!calibration.isCalibrated) {
            // Default uncalibrated 1:1 mapping (Z->Y for Up, Y->-Z for forward)
            return Triple(serverPos.x.toFloat(), serverPos.z.toFloat(), -serverPos.y.toFloat())
        }

        val dx = (serverPos.x - calibration.serverAnchorX).toFloat()
        val dy = (serverPos.y - calibration.serverAnchorY).toFloat()
        val dz = (serverPos.z - calibration.serverAnchorZ).toFloat()

        val rad = Math.toRadians(calibration.rotationYawDegrees.toDouble())
        val cosA = cos(rad).toFloat()
        val sinA = sin(rad).toFloat()

        // In ARCore: +Y is Up, ground plane is (X, Z) with -Z being forward
        val arX = calibration.arOriginX + (dx * cosA - dy * sinA)
        val arZ = calibration.arOriginZ - (dx * sinA + dy * cosA)
        val arY = calibration.arOriginY + dz

        return Triple(arX, arY, arZ)
    }

    /**
     * Converts an ARCore camera position (X, Y, Z in meters)
     * into Server Local Coordinates (East=X, North=Y, Up=Z in meters).
     */
    fun arToServer(arX: Float, arY: Float, arZ: Float): APPosition {
        if (!calibration.isCalibrated) {
            return APPosition(x = arX.toDouble(), y = (-arZ).toDouble(), z = arY.toDouble())
        }

        val dxAr = arX - calibration.arOriginX
        val dzAr = arZ - calibration.arOriginZ
        val dyAr = arY - calibration.arOriginY

        val rad = Math.toRadians(calibration.rotationYawDegrees.toDouble())
        val cosA = cos(rad).toFloat()
        val sinA = sin(rad).toFloat()

        // Inverse 2D rotation on ground plane
        val dxServer = dxAr * cosA - dzAr * sinA
        val dyServer = -(dxAr * sinA + dzAr * cosA)

        val serverX = calibration.serverAnchorX + dxServer
        val serverY = calibration.serverAnchorY + dyServer
        val serverZ = calibration.serverAnchorZ + dyAr

        return APPosition(serverX, serverY, serverZ)
    }
}
