package com.example.wifiapp.models

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

// --- Wi-Fi Scan Models ---

@Serializable
data class ObservationEntry(
    val bssid: String,
    val ssid: String,
    @SerialName("rssi_dbm") val rssiDbm: Int? = null,
    @SerialName("link_quality") val linkQuality: Int? = null,
    @SerialName("frequency_mhz") val frequencyMhz: Int? = null,
    val channel: Int? = null
)

data class LocalWifiScan(
    val bssid: String,
    val ssid: String,
    val rssiDbm: Int,
    val frequencyMhz: Int,
    val channel: Int,
    val timestampMs: Long = System.currentTimeMillis()
)

// --- Server WebSocket Protocol Messages ---

@Serializable
data class MobileRegisterMessage(
    val type: String = "mobile_register",
    @SerialName("device_id") val deviceId: String,
    @SerialName("device_type") val deviceType: String = "android",
    val platform: String = "android",
    @SerialName("app_version") val appVersion: String = "1.0.0",
    @SerialName("api_key") val apiKey: String = ""
)

@Serializable
data class MobileRegisteredResponse(
    val type: String,
    @SerialName("device_id") val deviceId: String? = null,
    val status: String? = null,
    @SerialName("server_time") val serverTime: String? = null
)

@Serializable
data class MobilePosePayload(
    @SerialName("coordinate_system") val coordinateSystem: String = "local",
    val x: Double,
    val y: Double,
    val z: Double
)

@Serializable
data class MobileOrientationPayload(
    val qx: Double,
    val qy: Double,
    val qz: Double,
    val qw: Double
)

@Serializable
data class MobilePoseMessage(
    val type: String = "mobile_pose",
    @SerialName("device_id") val deviceId: String,
    val timestamp: String,
    @SerialName("coordinate_system") val coordinateSystem: String = "arcore",
    val position: MobilePosePayload,
    val orientation: MobileOrientationPayload,
    @SerialName("tracking_state") val trackingState: String = "TRACKING"
)

@Serializable
data class MobileWiFiObservationsMessage(
    val type: String = "mobile_wifi_observations",
    @SerialName("device_id") val deviceId: String,
    val timestamp: String,
    val observations: List<ObservationEntry>,
    val pose: MobilePosePayload
)

@Serializable
data class HeartbeatMessage(
    val type: String = "heartbeat",
    @SerialName("hub_id") val hubId: String,
    val timestamp: String
)

@Serializable
data class APPosition(
    val x: Double = 0.0,
    val y: Double = 0.0,
    val z: Double = 0.0
)

@Serializable
data class APUpdatePayload(
    val bssid: String,
    val ssid: String = "<hidden>",
    val position: APPosition? = null,
    @SerialName("coordinate_system") val coordinateSystem: String = "local",
    val confidence: Double = 0.0,
    @SerialName("error_radius_m") val errorRadiusM: Double = 0.0,
    val quality: String = "low",
    val status: String = "unknown",
    @SerialName("hub_count") val hubCount: Int = 0,
    @SerialName("observation_count") val observationCount: Long = 0,
    @SerialName("last_seen") val lastSeen: String? = null
)

@Serializable
data class APUpdateMessage(
    val type: String = "ap_update",
    val ap: APUpdatePayload
)

@Serializable
data class HubPayload(
    @SerialName("hub_id") val hubId: String,
    @SerialName("device_type") val deviceType: String = "laptop",
    val platform: String = "windows",
    val version: String = "1.0.0",
    val status: String = "online",
    @SerialName("coordinate_system") val coordinateSystem: String? = null,
    val position: APPosition? = null,
    @SerialName("observation_count") val observationCount: Long = 0,
    @SerialName("last_seen") val lastSeen: String? = null
)

@Serializable
data class HubsSnapshotMessage(
    val type: String = "hubs_snapshot",
    val hubs: List<HubPayload> = emptyList()
)

@Serializable
data class HubUpdateMessage(
    val type: String = "hub_update",
    val hub: HubPayload
)

@Serializable
data class UpdateHubPositionMessage(
    val type: String = "update_hub_position",
    @SerialName("hub_id") val hubId: String,
    @SerialName("coordinate_system") val coordinateSystem: String = "local",
    val x: Double,
    val y: Double,
    val z: Double
)

data class HubUIState(
    val hubId: String,
    val deviceType: String = "laptop",
    val platform: String = "windows",
    val version: String = "1.0.0",
    val status: String = "online",
    val serverPosition: APPosition? = null,
    val arPositionX: Float? = null,
    val arPositionY: Float? = null,
    val arPositionZ: Float? = null,
    val distanceToUserM: Float? = null,
    val observationCount: Long = 0,
    val isCalibrated: Boolean = false,
    val isSelected: Boolean = false,
    val lastSeen: String? = null,
    val lastUpdatedMs: Long = System.currentTimeMillis()
) {
    val serverPositionX: Double get() = serverPosition?.x ?: 0.0
    val serverPositionY: Double get() = serverPosition?.y ?: 0.0
    val serverPositionZ: Double get() = serverPosition?.z ?: 0.0
}

// --- Enums & UI State ---

enum class ConnectionStatus {
    DISCONNECTED,
    CONNECTING,
    CONNECTED,
    RECONNECTING,
    AUTHENTICATION_FAILED,
    SERVER_UNAVAILABLE
}

enum class APLocalizationStatus {
    LOCALIZED,
    PREDICTED_RSSI,
    UNSTABLE,
    INSUFFICIENT_DATA,
    STALE,
    UNKNOWN;

    companion object {
        fun fromString(value: String): APLocalizationStatus = when (value.lowercase()) {
            "localized" -> LOCALIZED
            "predicted_rssi" -> PREDICTED_RSSI
            "unstable" -> UNSTABLE
            "insufficient_data" -> INSUFFICIENT_DATA
            "stale" -> STALE
            else -> UNKNOWN
        }
    }
}

data class AccessPointUIState(
    val bssid: String,
    val ssid: String,
    // Server-space coordinates (m)
    val serverPosition: APPosition? = null,
    // Transformed ARCore-space coordinates (m)
    val arPositionX: Float? = null,
    val arPositionY: Float? = null,
    val arPositionZ: Float? = null,
    val confidence: Double = 0.0,
    val errorRadiusM: Double = 0.0,
    val status: APLocalizationStatus = APLocalizationStatus.UNKNOWN,
    val quality: String = "low",
    val hubCount: Int = 0,
    val observationCount: Long = 0,
    // Phone-measured local observation (Real RSSI in dBm)
    val phoneRssiDbm: Int? = null,
    val phoneRssiHistory: List<Int> = emptyList(),
    val phoneFrequencyMhz: Int? = null,
    val phoneChannel: Int? = null,
    val phoneScanAgeMs: Long = 0L,
    // Calculated distance in meters from camera pose to estimated AP position
    val distanceToUserM: Float? = null,
    val isSelected: Boolean = false,
    val lastUpdatedMs: Long = System.currentTimeMillis()
) {
    val hasSpatialPosition: Boolean
        get() = arPositionX != null && arPositionY != null && arPositionZ != null

    val isLocalized: Boolean
        get() = (status == APLocalizationStatus.LOCALIZED || status == APLocalizationStatus.UNSTABLE) && serverPosition != null && hasSpatialPosition

    // Helper for RSSI trend ("Getting closer", "Getting farther", "Stable")
    val rssiTrend: String
        get() {
            if (phoneRssiHistory.size < 2) return "Stable"
            val recent = phoneRssiHistory.takeLast(4)
            val diff = recent.last() - recent.first()
            return when {
                diff >= 4 -> "Signal getting stronger (approaching)"
                diff <= -4 -> "Signal getting weaker (moving away)"
                else -> "Stable signal"
            }
        }
}

data class CalibrationState(
    val isCalibrated: Boolean = false,
    val anchorId: String = "ORIGIN",
    val serverAnchorX: Double = 0.0,
    val serverAnchorY: Double = 0.0,
    val serverAnchorZ: Double = 0.0,
    val arOriginX: Float = 0f,
    val arOriginY: Float = 0f,
    val arOriginZ: Float = 0f,
    val rotationYawDegrees: Float = 0f,
    val calibratedAtMs: Long = 0L
)

data class TimestampedPose(
    val timestampMs: Long,
    val x: Float,
    val y: Float,
    val z: Float,
    val qx: Float,
    val qy: Float,
    val qz: Float,
    val qw: Float,
    val isTracking: Boolean
)

class PoseHistoryBuffer(private val maxDurationMs: Long = 10_000L) {
    private val buffer = java.util.concurrent.ConcurrentLinkedDeque<TimestampedPose>()

    fun addPose(pose: TimestampedPose) {
        buffer.addLast(pose)
        val cutoff = pose.timestampMs - maxDurationMs
        while (buffer.isNotEmpty() && buffer.first.timestampMs < cutoff) {
            buffer.pollFirst()
        }
    }

    fun getClosestPose(timestampMs: Long, maxToleranceMs: Long = 4_000L): TimestampedPose? {
        if (buffer.isEmpty()) return null
        var closest: TimestampedPose? = null
        var minDiff = Long.MAX_VALUE
        for (pose in buffer) {
            val diff = kotlin.math.abs(pose.timestampMs - timestampMs)
            if (diff < minDiff) {
                minDiff = diff
                closest = pose
            }
        }
        return if (minDiff <= maxToleranceMs) closest else null
    }

    fun clear() {
        buffer.clear()
    }
}
