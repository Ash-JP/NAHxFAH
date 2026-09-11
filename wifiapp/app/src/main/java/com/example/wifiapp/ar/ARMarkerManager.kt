package com.example.wifiapp.ar

import com.example.wifiapp.models.APLocalizationStatus
import com.example.wifiapp.models.APUpdatePayload
import com.example.wifiapp.models.AccessPointUIState
import com.example.wifiapp.models.LocalWifiScan
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlin.math.atan2
import kotlin.math.sqrt

data class ScreenPoint(
    val screenX: Float,
    val screenY: Float,
    val isVisibleInFov: Boolean,
    val edgeAngleDegrees: Float = 0f
)

class ARMarkerManager(
    private val transformer: CoordinateTransformer
) {
    private val _accessPoints = MutableStateFlow<Map<String, AccessPointUIState>>(emptyMap())
    val accessPoints: StateFlow<Map<String, AccessPointUIState>> = _accessPoints.asStateFlow()

    private val _selectedBssid = MutableStateFlow<String?>(null)
    val selectedBssid: StateFlow<String?> = _selectedBssid.asStateFlow()

    private val scope = CoroutineScope(Dispatchers.Default)

    fun onAPUpdateReceived(update: APUpdatePayload) {
        scope.launch {
            val current = _accessPoints.value.toMutableMap()
            val existing = current[update.bssid]

            val serverPos = update.position
            var arX: Float? = null
            var arY: Float? = null
            var arZ: Float? = null

            if (serverPos != null) {
                val targetAr = transformer.serverToAr(serverPos)
                if (existing != null && existing.arPositionX != null && existing.arPositionY != null && existing.arPositionZ != null) {
                    // Smooth interpolation (alpha lerp = 0.5) to avoid abrupt jumping
                    arX = existing.arPositionX + 0.5f * (targetAr.first - existing.arPositionX)
                    arY = existing.arPositionY + 0.5f * (targetAr.second - existing.arPositionY)
                    arZ = existing.arPositionZ + 0.5f * (targetAr.third - existing.arPositionZ)
                } else {
                    arX = targetAr.first
                    arY = targetAr.second
                    arZ = targetAr.third
                }
            }

            val status = APLocalizationStatus.fromString(update.status)
            val updatedState = (existing ?: AccessPointUIState(bssid = update.bssid, ssid = update.ssid)).copy(
                ssid = if (update.ssid.isNotEmpty() && update.ssid != "<hidden>") update.ssid else existing?.ssid ?: update.ssid,
                serverPosition = serverPos,
                arPositionX = arX,
                arPositionY = arY,
                arPositionZ = arZ,
                confidence = update.confidence,
                errorRadiusM = update.errorRadiusM,
                status = status,
                quality = update.quality,
                hubCount = update.hubCount,
                observationCount = update.observationCount,
                lastUpdatedMs = System.currentTimeMillis()
            )

            current[update.bssid] = updatedState
            _accessPoints.value = current
        }
    }

    fun onLocalWifiScanResults(scans: List<LocalWifiScan>) {
        scope.launch {
            val current = _accessPoints.value.toMutableMap()
            val scanMap = scans.associateBy { it.bssid }

            // 1. Update existing APs with newly observed phone RSSI and history
            for ((bssid, apState) in current) {
                val localScan = scanMap[bssid]
                if (localScan != null) {
                    val history = (apState.phoneRssiHistory + localScan.rssiDbm).takeLast(10)
                    current[bssid] = apState.copy(
                        ssid = if (localScan.ssid != "<hidden>") localScan.ssid else apState.ssid,
                        phoneRssiDbm = localScan.rssiDbm,
                        phoneRssiHistory = history,
                        phoneFrequencyMhz = localScan.frequencyMhz,
                        phoneChannel = localScan.channel,
                        phoneScanAgeMs = 0L
                    )
                } else {
                    // Increment scan age
                    current[bssid] = apState.copy(phoneScanAgeMs = apState.phoneScanAgeMs + 6000L)
                }
            }

            // 2. Add unlocalized APs detected only by the phone
            for (scan in scans) {
                if (!current.containsKey(scan.bssid)) {
                    current[scan.bssid] = AccessPointUIState(
                        bssid = scan.bssid,
                        ssid = scan.ssid,
                        status = APLocalizationStatus.INSUFFICIENT_DATA,
                        phoneRssiDbm = scan.rssiDbm,
                        phoneRssiHistory = listOf(scan.rssiDbm),
                        phoneFrequencyMhz = scan.frequencyMhz,
                        phoneChannel = scan.channel,
                        phoneScanAgeMs = 0L
                    )
                }
            }

            _accessPoints.value = current
        }
    }

    fun updateCameraDistances(camX: Float, camY: Float, camZ: Float) {
        val current = _accessPoints.value
        if (current.isEmpty()) return

        val updated = current.mapValues { (_, ap) ->
            val ax = ap.arPositionX
            val ay = ap.arPositionY
            val az = ap.arPositionZ
            if (ax != null && ay != null && az != null) {
                val dx = ax - camX
                val dy = ay - camY
                val dz = az - camZ
                val dist = sqrt(dx * dx + dy * dy + dz * dz)
                ap.copy(distanceToUserM = dist)
            } else {
                ap
            }
        }
        _accessPoints.value = updated
    }

    fun recalculateArCoordinates() {
        // Called after AR calibration changes
        val current = _accessPoints.value.toMutableMap()
        for ((bssid, ap) in current) {
            val serverPos = ap.serverPosition
            if (serverPos != null) {
                val (arX, arY, arZ) = transformer.serverToAr(serverPos)
                current[bssid] = ap.copy(arPositionX = arX, arPositionY = arY, arPositionZ = arZ)
            }
        }
        _accessPoints.value = current
    }

    fun selectAP(bssid: String?) {
        _selectedBssid.value = bssid
        val current = _accessPoints.value.toMutableMap()
        for ((k, ap) in current) {
            current[k] = ap.copy(isSelected = (k == bssid))
        }
        _accessPoints.value = current
    }

    /**
     * Projects an ARCore 3D position (X, Y, Z) to 2D screen coordinates
     * using viewMatrix and projectionMatrix (each 16 floats in OpenGL column-major order).
     */
    fun projectToScreen(
        posX: Float,
        posY: Float,
        posZ: Float,
        viewMatrix: FloatArray,
        projMatrix: FloatArray,
        screenWidth: Float,
        screenHeight: Float
    ): ScreenPoint {
        // Transform point by view matrix
        val vx = viewMatrix[0] * posX + viewMatrix[4] * posY + viewMatrix[8] * posZ + viewMatrix[12]
        val vy = viewMatrix[1] * posX + viewMatrix[5] * posY + viewMatrix[9] * posZ + viewMatrix[13]
        val vz = viewMatrix[2] * posX + viewMatrix[6] * posY + viewMatrix[10] * posZ + viewMatrix[14]
        val vw = viewMatrix[3] * posX + viewMatrix[7] * posY + viewMatrix[11] * posZ + viewMatrix[15]

        // Transform by projection matrix
        val clipX = projMatrix[0] * vx + projMatrix[4] * vy + projMatrix[8] * vz + projMatrix[12] * vw
        val clipY = projMatrix[1] * vx + projMatrix[5] * vy + projMatrix[9] * vz + projMatrix[13] * vw
        val clipZ = projMatrix[2] * vx + projMatrix[6] * vy + projMatrix[10] * vz + projMatrix[14] * vw
        val clipW = projMatrix[3] * vx + projMatrix[7] * vy + projMatrix[11] * vz + projMatrix[15] * vw

        val isBehind = clipW <= 0.001f

        // Normalized Device Coordinates (NDC) in [-1, 1]
        val ndcX = clipX / clipW
        val ndcY = clipY / clipW

        val inFov = !isBehind && ndcX in -1.0f..1.0f && ndcY in -1.0f..1.0f

        return if (inFov) {
            val sx = (ndcX + 1f) * 0.5f * screenWidth
            val sy = (1f - ndcY) * 0.5f * screenHeight
            ScreenPoint(screenX = sx, screenY = sy, isVisibleInFov = true)
        } else {
            // Point is outside screen bounds or behind camera.
            // Compute edge clamp and angle for directional arrow indicator
            val dirX = if (isBehind) -ndcX else ndcX
            val dirY = if (isBehind) -ndcY else ndcY
            val angle = Math.toDegrees(atan2(dirY.toDouble(), dirX.toDouble())).toFloat()

            val cx = screenWidth * 0.5f
            val cy = screenHeight * 0.5f
            val pad = 64f

            val edgeX = (cx + dirX * (cx - pad)).coerceIn(pad, screenWidth - pad)
            val edgeY = (cy - dirY * (cy - pad)).coerceIn(pad, screenHeight - pad)

            ScreenPoint(screenX = edgeX, screenY = edgeY, isVisibleInFov = false, edgeAngleDegrees = angle)
        }
    }
}
