package com.example.wifiapp.network

import android.content.Context
import android.util.Log
import com.example.wifiapp.identity.IdentityManager
import com.example.wifiapp.models.*
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.*
import okio.ByteString
import java.text.SimpleDateFormat
import java.util.*
import java.util.concurrent.TimeUnit

class ServerWebSocketManager(private val context: Context) {
    companion object {
        private const val TAG = "ServerWebSocket"
        private const val HEARTBEAT_INTERVAL_MS = 5000L
        private const val MIN_POSE_INTERVAL_MS = 100L // 10 Hz rate limit
    }

    private val json = Json {
        ignoreUnknownKeys = true
        isLenient = true
        encodeDefaults = true
    }

    private val okHttpClient = OkHttpClient.Builder()
        .readTimeout(10, TimeUnit.SECONDS)
        .writeTimeout(10, TimeUnit.SECONDS)
        .pingInterval(10, TimeUnit.SECONDS)
        .build()

    private var webSocket: WebSocket? = null
    private val scope = CoroutineScope(Dispatchers.IO + SupervisorJob())

    private val deviceId = IdentityManager.getDeviceId(context)

    private val _connectionState = MutableStateFlow(ConnectionStatus.DISCONNECTED)
    val connectionState: StateFlow<ConnectionStatus> = _connectionState.asStateFlow()

    private val _apUpdates = MutableSharedFlow<APUpdatePayload>(replay = 50, extraBufferCapacity = 100)
    val apUpdates: SharedFlow<APUpdatePayload> = _apUpdates.asSharedFlow()

    private val _hubsState = MutableStateFlow<Map<String, HubUIState>>(emptyMap())
    val hubsState: StateFlow<Map<String, HubUIState>> = _hubsState.asStateFlow()

    private val _lastMessageTime = MutableStateFlow(0L)
    val lastMessageTime: StateFlow<Long> = _lastMessageTime.asStateFlow()

    private var currentUrl: String = "ws://10.0.2.2:8000/ws"
    private var isUserDisconnected = false
    private var reconnectAttempt = 0
    private var reconnectJob: Job? = null
    private var heartbeatJob: Job? = null
    private var lastPoseSentTime = 0L

    fun connect(url: String) {
        currentUrl = url.trim()
        isUserDisconnected = false
        reconnectAttempt = 0
        reconnectJob?.cancel()
        initiateConnection()
    }

    fun disconnect() {
        isUserDisconnected = true
        reconnectJob?.cancel()
        heartbeatJob?.cancel()
        webSocket?.close(1000, "User disconnected")
        webSocket = null
        _connectionState.value = ConnectionStatus.DISCONNECTED
    }

    private fun initiateConnection() {
        if (_connectionState.value == ConnectionStatus.CONNECTED) return

        _connectionState.value = if (reconnectAttempt == 0) ConnectionStatus.CONNECTING else ConnectionStatus.RECONNECTING
        Log.d(TAG, "Connecting to $currentUrl (attempt #$reconnectAttempt)")

        val request = try {
            Request.Builder().url(currentUrl).build()
        } catch (e: Exception) {
            Log.e(TAG, "Invalid server URL: ${e.message}")
            _connectionState.value = ConnectionStatus.SERVER_UNAVAILABLE
            return
        }

        webSocket = okHttpClient.newWebSocket(request, object : WebSocketListener() {
            override fun onOpen(ws: WebSocket, response: Response) {
                Log.d(TAG, "WebSocket connected successfully to $currentUrl")
                _connectionState.value = ConnectionStatus.CONNECTED
                reconnectAttempt = 0

                // 1. Send mobile_register handshake
                sendRegister()

                // 2. Start periodic heartbeat
                startHeartbeat()
            }

            override fun onMessage(ws: WebSocket, text: String) {
                _lastMessageTime.value = System.currentTimeMillis()
                handleIncomingMessage(text)
            }

            override fun onMessage(ws: WebSocket, bytes: ByteString) {
                _lastMessageTime.value = System.currentTimeMillis()
            }

            override fun onClosing(ws: WebSocket, code: Int, reason: String) {
                Log.d(TAG, "Server closing WebSocket: $code / $reason")
            }

            override fun onClosed(ws: WebSocket, code: Int, reason: String) {
                Log.d(TAG, "WebSocket closed: $code / $reason")
                _connectionState.value = ConnectionStatus.DISCONNECTED
                heartbeatJob?.cancel()
                if (!isUserDisconnected) {
                    scheduleReconnect()
                }
            }

            override fun onFailure(ws: WebSocket, t: Throwable, response: Response?) {
                Log.e(TAG, "WebSocket failure: ${t.message}")
                _connectionState.value = ConnectionStatus.SERVER_UNAVAILABLE
                heartbeatJob?.cancel()
                if (!isUserDisconnected) {
                    scheduleReconnect()
                }
            }
        })
    }

    private fun scheduleReconnect() {
        reconnectJob?.cancel()
        reconnectAttempt++
        // Backoff: 1s, 2s, 4s, 8s, 16s, capped at 30s
        val delaySec = when (reconnectAttempt) {
            1 -> 1L
            2 -> 2L
            3 -> 4L
            4 -> 8L
            5 -> 16L
            else -> 30L
        }
        Log.d(TAG, "Scheduling reconnect in $delaySec seconds...")
        reconnectJob = scope.launch {
            delay(delaySec * 1000L)
            if (!isUserDisconnected && _connectionState.value != ConnectionStatus.CONNECTED) {
                initiateConnection()
            }
        }
    }

    private fun sendRegister() {
        val registerMsg = MobileRegisterMessage(
            deviceId = deviceId,
            deviceType = "android",
            platform = "android",
            appVersion = "1.0.0"
        )
        val payload = json.encodeToString(registerMsg)
        webSocket?.send(payload)
        Log.d(TAG, "Sent mobile_register: $payload")
    }

    private fun startHeartbeat() {
        heartbeatJob?.cancel()
        heartbeatJob = scope.launch {
            while (isActive) {
                delay(HEARTBEAT_INTERVAL_MS)
                if (_connectionState.value == ConnectionStatus.CONNECTED) {
                    val nowIso = getIsoTimestamp()
                    val heartbeat = HeartbeatMessage(hubId = deviceId, timestamp = nowIso)
                    webSocket?.send(json.encodeToString(heartbeat))
                }
            }
        }
    }

    private fun handleIncomingMessage(text: String) {
        try {
            val element = json.parseToJsonElement(text).jsonObject
            val type = element["type"]?.jsonPrimitive?.content ?: return

            when (type) {
                "ap_update" -> {
                    val msg = json.decodeFromString<APUpdateMessage>(text)
                    scope.launch { _apUpdates.emit(msg.ap) }
                }
                "hubs_snapshot" -> {
                    val snapshot = json.decodeFromString<HubsSnapshotMessage>(text)
                    val map = snapshot.hubs.associate { hub ->
                        hub.hubId to HubUIState(
                            hubId = hub.hubId,
                            deviceType = hub.deviceType,
                            platform = hub.platform,
                            version = hub.version,
                            status = hub.status,
                            serverPosition = hub.position,
                            observationCount = hub.observationCount,
                            isCalibrated = hub.position != null && (hub.position.x != 0.0 || hub.position.y != 0.0 || hub.position.z != 0.0),
                            lastSeen = hub.lastSeen
                        )
                    }
                    _hubsState.value = map
                    Log.i(TAG, "Received hubs snapshot with ${map.size} venue hubs")
                }
                "hub_update" -> {
                    val msg = json.decodeFromString<HubUpdateMessage>(text)
                    val hub = msg.hub
                    val current = _hubsState.value.toMutableMap()
                    val existing = current[hub.hubId]
                    current[hub.hubId] = HubUIState(
                        hubId = hub.hubId,
                        deviceType = if (hub.deviceType.isNotEmpty()) hub.deviceType else existing?.deviceType ?: "laptop",
                        platform = if (hub.platform.isNotEmpty()) hub.platform else existing?.platform ?: "windows",
                        version = if (hub.version.isNotEmpty()) hub.version else existing?.version ?: "1.0.0",
                        status = if (hub.status.isNotEmpty()) hub.status else existing?.status ?: "online",
                        serverPosition = hub.position ?: existing?.serverPosition,
                        observationCount = if (hub.observationCount > 0) hub.observationCount else existing?.observationCount ?: 0L,
                        isCalibrated = (hub.position != null && (hub.position.x != 0.0 || hub.position.y != 0.0 || hub.position.z != 0.0)) || existing?.isCalibrated == true,
                        lastSeen = hub.lastSeen ?: existing?.lastSeen,
                        arPositionX = existing?.arPositionX,
                        arPositionY = existing?.arPositionY,
                        arPositionZ = existing?.arPositionZ,
                        distanceToUserM = existing?.distanceToUserM,
                        isSelected = existing?.isSelected ?: false
                    )
                    _hubsState.value = current
                    Log.i(TAG, "Venue hub updated: ${hub.hubId} (status: ${hub.status}, pos: ${hub.position})")
                }
                "mobile_registered" -> {
                    Log.i(TAG, "Mobile registered successfully on server!")
                }
                "heartbeat_ack" -> {
                    // Ack received
                }
                "error" -> {
                    Log.w(TAG, "Server error message: $text")
                }
                else -> {
                    Log.d(TAG, "Unhandled message type: $type")
                }
            }
        } catch (e: Exception) {
            Log.e(TAG, "Error parsing server message: ${e.message} in: $text")
        }
    }

    /**
     * Calibrates / updates a hub's physical location in the venue.
     */
    fun sendUpdateHubPosition(hubId: String, x: Double, y: Double, z: Double, cs: String = "local") {
        val msg = UpdateHubPositionMessage(
            hubId = hubId,
            coordinateSystem = cs,
            x = x,
            y = y,
            z = z
        )
        val payload = json.encodeToString(msg)
        if (_connectionState.value == ConnectionStatus.CONNECTED) {
            webSocket?.send(payload)
            Log.i(TAG, "Sent update_hub_position for $hubId: $payload")
        }

        // Optimistically update local state
        val current = _hubsState.value.toMutableMap()
        val existing = current[hubId]
        current[hubId] = (existing ?: HubUIState(hubId = hubId)).copy(
            serverPosition = APPosition(x, y, z),
            isCalibrated = true,
            status = "online",
            lastUpdatedMs = System.currentTimeMillis()
        )
        _hubsState.value = current
    }

    /**
     * Sends ARCore device pose (throttled to 10 Hz).
     */
    fun sendPose(arX: Float, arY: Float, arZ: Float, qx: Float, qy: Float, qz: Float, qw: Float, trackingState: String) {
        if (_connectionState.value != ConnectionStatus.CONNECTED) return
        val now = System.currentTimeMillis()
        if (now - lastPoseSentTime < MIN_POSE_INTERVAL_MS) return
        lastPoseSentTime = now

        val poseMsg = MobilePoseMessage(
            deviceId = deviceId,
            timestamp = getIsoTimestamp(),
            position = MobilePosePayload(
                coordinateSystem = "arcore",
                x = arX.toDouble(),
                y = arY.toDouble(),
                z = arZ.toDouble()
            ),
            orientation = MobileOrientationPayload(
                qx = qx.toDouble(),
                qy = qy.toDouble(),
                qz = qz.toDouble(),
                qw = qw.toDouble()
            ),
            trackingState = trackingState
        )
        webSocket?.send(json.encodeToString(poseMsg))
    }

    /**
     * Sends Wi-Fi observations along with estimated server-world pose.
     */
    fun sendWifiObservations(scans: List<LocalWifiScan>, serverPose: APPosition) {
        if (_connectionState.value != ConnectionStatus.CONNECTED || scans.isEmpty()) return

        val entries = scans.map { scan ->
            ObservationEntry(
                bssid = scan.bssid,
                ssid = scan.ssid,
                rssiDbm = scan.rssiDbm,
                linkQuality = calculateLinkQuality(scan.rssiDbm),
                frequencyMhz = scan.frequencyMhz,
                channel = scan.channel
            )
        }

        val obsMsg = MobileWiFiObservationsMessage(
            deviceId = deviceId,
            timestamp = getIsoTimestamp(),
            observations = entries,
            pose = MobilePosePayload(
                coordinateSystem = "local",
                x = serverPose.x,
                y = serverPose.y,
                z = serverPose.z
            )
        )

        val payload = json.encodeToString(obsMsg)
        webSocket?.send(payload)
        Log.d(TAG, "Dispatched ${entries.size} mobile Wi-Fi observations at ($serverPose)")
    }

    private fun calculateLinkQuality(rssiDbm: Int): Int {
        // Map [-100, -50] dBm to [0, 100]
        return when {
            rssiDbm >= -50 -> 100
            rssiDbm <= -100 -> 0
            else -> 2 * (rssiDbm + 100)
        }
    }

    private fun getIsoTimestamp(): String {
        val sdf = SimpleDateFormat("yyyy-MM-dd'T'HH:mm:ss.SSS'Z'", Locale.US)
        sdf.timeZone = TimeZone.getTimeZone("UTC")
        return sdf.format(Date())
    }
}
