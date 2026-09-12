package com.example.wifiapp.ar

import com.example.wifiapp.models.*
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

    private val _hubs = MutableStateFlow<Map<String, HubUIState>>(emptyMap())
    val hubs: StateFlow<Map<String, HubUIState>> = _hubs.asStateFlow()

    private val _selectedHubId = MutableStateFlow<String?>(null)
    val selectedHubId: StateFlow<String?> = _selectedHubId.asStateFlow()

    private val scope = CoroutineScope(Dispatchers.Default)

    fun updateHubs(newHubs: Map<String, HubUIState>) {
        val current = _hubs.value.toMutableMap()
        for ((id, hub) in newHubs) {
            val existing = current[id]
            val serverPos = hub.serverPosition
            var arX: Float? = null
            var arY: Float? = null
            var arZ: Float? = null
            if (serverPos != null && (serverPos.x != 0.0 || serverPos.y != 0.0 || serverPos.z != 0.0)) {
                val targetAr = transformer.serverToAr(serverPos)
                arX = targetAr.first
                arY = targetAr.second
                arZ = targetAr.third
            } else if (existing?.arPositionX != null) {
                arX = existing.arPositionX
                arY = existing.arPositionY
                arZ = existing.arPositionZ
            }
            current[id] = hub.copy(
                arPositionX = arX,
                arPositionY = arY,
                arPositionZ = arZ,
                isCalibrated = arX != null && arY != null && arZ != null
            )
        }
        _hubs.value = current
    }

    fun selectHub(hubId: String?) {
        _selectedHubId.value = hubId
        val current = _hubs.value.toMutableMap()
        for ((k, h) in current) {
            current[k] = h.copy(isSelected = (k == hubId))
        }
        _hubs.value = current
    }

    fun onAPUpdateReceived(update: APUpdatePayload) {
        scope.launch {
            val current = _accessPoints.value.toMutableMap()
            val existing = current[update.bssid]

            val serverPos = update.position
            val status = APLocalizationStatus.fromString(update.status)
            var arX: Float? = null
            var arY: Float? = null
            var arZ: Float? = null

            // Rule 38: Only create a physical AR marker when the server provides a valid localized position
            if (serverPos != null && (status == APLocalizationStatus.LOCALIZED || status == APLocalizationStatus.UNSTABLE)) {
                val targetAr = transformer.serverToAr(serverPos)
                if (existing != null && existing.arPositionX != null && existing.arPositionY != null && existing.arPositionZ != null) {
                    // Smooth interpolation (Section 25)
                    val alpha = 0.35f
                    arX = existing.arPositionX + alpha * (targetAr.first - existing.arPositionX)
                    arY = existing.arPositionY + alpha * (targetAr.second - existing.arPositionY)
                    arZ = existing.arPositionZ + alpha * (targetAr.third - existing.arPositionZ)
                } else {
                    arX = targetAr.first
                    arY = targetAr.second
                    arZ = targetAr.third
                }
            } else {
                // If insufficient data or no server position, no physical AR marker
                arX = null
                arY = null
                arZ = null
            }

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

            // 1. Update existing APs with newly observed phone RSSI
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
                        phoneScanAgeMs = 0L,
                        lastUpdatedMs = System.currentTimeMillis()
                    )
                } else {
                    current[bssid] = apState.copy(phoneScanAgeMs = apState.phoneScanAgeMs + 6000L)
                }
            }

            // 2. Add newly discovered APs as INSUFFICIENT_DATA without fake 3D positions (Rule 38)
            for (scan in scans) {
                if (!current.containsKey(scan.bssid)) {
                    current[scan.bssid] = AccessPointUIState(
                        bssid = scan.bssid,
                        ssid = scan.ssid,
                        serverPosition = null,
                        arPositionX = null,
                        arPositionY = null,
                        arPositionZ = null,
                        confidence = 0.0,
                        errorRadiusM = 0.0,
                        status = APLocalizationStatus.INSUFFICIENT_DATA,
                        quality = "low",
                        hubCount = 1,
                        observationCount = 1,
                        phoneRssiDbm = scan.rssiDbm,
                        phoneRssiHistory = listOf(scan.rssiDbm),
                        phoneFrequencyMhz = scan.frequencyMhz,
                        phoneChannel = scan.channel,
                        phoneScanAgeMs = 0L,
                        lastUpdatedMs = System.currentTimeMillis()
                    )
                }
            }

            _accessPoints.value = current
        }
    }

    fun updateCameraDistances(camX: Float, camY: Float, camZ: Float) {
        val currentAPs = _accessPoints.value
        if (currentAPs.isNotEmpty()) {
            val updatedAPs = currentAPs.mapValues { (_, ap) ->
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
            _accessPoints.value = updatedAPs
        }

        val currentHubs = _hubs.value
        if (currentHubs.isNotEmpty()) {
            val updatedHubs = currentHubs.mapValues { (_, hub) ->
                val hx = hub.arPositionX
                val hy = hub.arPositionY
                val hz = hub.arPositionZ
                if (hx != null && hy != null && hz != null) {
                    val dx = hx - camX
                    val dy = hy - camY
                    val dz = hz - camZ
                    val dist = sqrt(dx * dx + dy * dy + dz * dz)
                    hub.copy(distanceToUserM = dist)
                } else {
                    hub
                }
            }
            _hubs.value = updatedHubs
        }
    }

    fun recalculateArCoordinates() {
        // Recalculate AP AR coordinates
        val currentAPs = _accessPoints.value.toMutableMap()
        for ((bssid, ap) in currentAPs) {
            val serverPos = ap.serverPosition
            if (serverPos != null) {
                val (arX, arY, arZ) = transformer.serverToAr(serverPos)
                currentAPs[bssid] = ap.copy(arPositionX = arX, arPositionY = arY, arPositionZ = arZ)
            }
        }
        _accessPoints.value = currentAPs

        // Recalculate Hub AR coordinates
        val currentHubs = _hubs.value.toMutableMap()
        for ((id, hub) in currentHubs) {
            val serverPos = hub.serverPosition
            if (serverPos != null && (serverPos.x != 0.0 || serverPos.y != 0.0 || serverPos.z != 0.0)) {
                val (arX, arY, arZ) = transformer.serverToAr(serverPos)
                currentHubs[id] = hub.copy(arPositionX = arX, arPositionY = arY, arPositionZ = arZ, isCalibrated = true)
            }
        }
        _hubs.value = currentHubs
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
        return try {
            if (screenWidth <= 0f || screenHeight <= 0f || viewMatrix.size < 16 || projMatrix.size < 16) {
                return ScreenPoint(screenX = -9999f, screenY = -9999f, isVisibleInFov = false)
            }
            // If matrices are uninitialized (all zeros)
            if (viewMatrix[0] == 0f && viewMatrix[5] == 0f && viewMatrix[10] == 0f) {
                return ScreenPoint(screenX = -9999f, screenY = -9999f, isVisibleInFov = false)
            }

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

            if (clipW.isNaN() || clipW.isInfinite() || kotlin.math.abs(clipW) < 1e-4f) {
                return ScreenPoint(screenX = -9999f, screenY = -9999f, isVisibleInFov = false)
            }

            val isBehind = clipW < 0.05f

            // Normalized Device Coordinates (NDC) in [-1, 1]
            val ndcX = (clipX / clipW).let { if (it.isNaN() || it.isInfinite()) 0f else it }
            val ndcY = (clipY / clipW).let { if (it.isNaN() || it.isInfinite()) 0f else it }

            val inFov = !isBehind && ndcX in -1.0f..1.0f && ndcY in -1.0f..1.0f

            return if (inFov) {
                val sx = ((ndcX + 1f) * 0.5f * screenWidth).coerceIn(0f, screenWidth)
                val sy = ((1f - ndcY) * 0.5f * screenHeight).coerceIn(0f, screenHeight)
                ScreenPoint(screenX = sx, screenY = sy, isVisibleInFov = true)
            } else {
                var dirX = if (isBehind) -ndcX else ndcX
                var dirY = if (isBehind) -ndcY else ndcY
                val len = kotlin.math.sqrt(dirX * dirX + dirY * dirY)
                if (len > 1e-4f) {
                    dirX /= len
                    dirY /= len
                } else {
                    dirX = 0f
                    dirY = -1f
                }

                val angle = Math.toDegrees(atan2(dirY.toDouble(), dirX.toDouble())).toFloat()
                val safeAngle = if (angle.isNaN() || angle.isInfinite()) 0f else angle

                val cx = screenWidth * 0.5f
                val cy = screenHeight * 0.5f
                val pad = 48f

                val edgeX = (cx + dirX * (cx - pad)).coerceIn(pad, (screenWidth - pad).coerceAtLeast(pad))
                val edgeY = (cy - dirY * (cy - pad)).coerceIn(pad, (screenHeight - pad).coerceAtLeast(pad))

                ScreenPoint(
                    screenX = if (edgeX.isNaN()) pad else edgeX,
                    screenY = if (edgeY.isNaN()) pad else edgeY,
                    isVisibleInFov = false,
                    edgeAngleDegrees = safeAngle
                )
            }
        } catch (e: Exception) {
            ScreenPoint(screenX = -9999f, screenY = -9999f, isVisibleInFov = false)
        }
    }
}
