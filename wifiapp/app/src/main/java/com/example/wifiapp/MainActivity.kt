package com.example.wifiapp

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.animation.*
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.rotate
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.core.content.ContextCompat
import com.example.wifiapp.ar.ARMarkerManager
import com.example.wifiapp.ar.CoordinateTransformer
import com.example.wifiapp.ar.ScreenPoint
import com.example.wifiapp.calibration.CalibrationManager
import com.example.wifiapp.models.*
import com.example.wifiapp.network.ServerWebSocketManager
import com.example.wifiapp.wifi.WifiScanner
import com.google.ar.core.TrackingState
import io.github.sceneview.ar.ARScene
import io.github.sceneview.node.Node
import io.github.sceneview.rememberEngine
import io.github.sceneview.rememberModelLoader
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.launch
import kotlin.math.roundToInt

class MainActivity : ComponentActivity() {

    private lateinit var wifiScanner: WifiScanner
    private lateinit var transformer: CoordinateTransformer
    private lateinit var calibrationManager: CalibrationManager
    private lateinit var webSocketManager: ServerWebSocketManager
    private lateinit var markerManager: ARMarkerManager

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        transformer = CoordinateTransformer()
        calibrationManager = CalibrationManager(transformer)
        webSocketManager = ServerWebSocketManager(this)
        markerManager = ARMarkerManager(transformer)
        wifiScanner = WifiScanner(this)

        setContent {
            MaterialTheme {
                Surface(
                    modifier = Modifier.fillMaxSize(),
                    color = MaterialTheme.colorScheme.background
                ) {
                    AppRoot(
                        webSocketManager = webSocketManager,
                        wifiScanner = wifiScanner,
                        markerManager = markerManager,
                        calibrationManager = calibrationManager,
                        transformer = transformer
                    )
                }
            }
        }
    }

    override fun onResume() {
        super.onResume()
        wifiScanner.start()
    }

    override fun onPause() {
        super.onPause()
        wifiScanner.stop()
    }

    override fun onDestroy() {
        super.onDestroy()
        webSocketManager.disconnect()
    }
}

@Composable
fun AppRoot(
    webSocketManager: ServerWebSocketManager,
    wifiScanner: WifiScanner,
    markerManager: ARMarkerManager,
    calibrationManager: CalibrationManager,
    transformer: CoordinateTransformer
) {
    val context = LocalContext.current

    val requiredPermissions = remember {
        val list = mutableListOf(
            Manifest.permission.CAMERA,
            Manifest.permission.ACCESS_FINE_LOCATION,
            Manifest.permission.ACCESS_COARSE_LOCATION
        )
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            list.add(Manifest.permission.NEARBY_WIFI_DEVICES)
        }
        list.toTypedArray()
    }

    var allPermissionsGranted by remember {
        mutableStateOf(
            requiredPermissions.all {
                ContextCompat.checkSelfPermission(context, it) == PackageManager.PERMISSION_GRANTED
            }
        )
    }

    val launcher = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestMultiplePermissions()
    ) { results ->
        allPermissionsGranted = results.values.all { it }
    }

    if (!allPermissionsGranted) {
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(24.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.Center
        ) {
            Text(
                text = "📡",
                fontSize = 54.sp
            )
            Spacer(modifier = Modifier.height(16.dp))
            Text(
                text = "WIFI HUNTER AR",
                style = MaterialTheme.typography.headlineMedium,
                fontWeight = FontWeight.Bold
            )
            Spacer(modifier = Modifier.height(8.dp))
            Text(
                text = "Requires Camera for ARCore spatial tracking and Location/Wi-Fi permissions for real Wi-Fi observation scanning.",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant
            )
            Spacer(modifier = Modifier.height(24.dp))
            Button(
                onClick = { launcher.launch(requiredPermissions) },
                modifier = Modifier.fillMaxWidth(0.8f)
            ) {
                Text("Grant Permissions")
            }
        }
    } else {
        MainARScreen(
            webSocketManager = webSocketManager,
            wifiScanner = wifiScanner,
            markerManager = markerManager,
            calibrationManager = calibrationManager,
            transformer = transformer
        )
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MainARScreen(
    webSocketManager: ServerWebSocketManager,
    wifiScanner: WifiScanner,
    markerManager: ARMarkerManager,
    calibrationManager: CalibrationManager,
    transformer: CoordinateTransformer
) {
    val context = LocalContext.current
    val coroutineScope = rememberCoroutineScope()
    val density = LocalDensity.current
    val configuration = LocalConfiguration.current

    val screenWidthPx = with(density) { configuration.screenWidthDp.dp.toPx() }
    val screenHeightPx = with(density) { configuration.screenHeightDp.dp.toPx() }

    // Persistent server URL
    val prefs = remember { context.getSharedPreferences("wifi_hunter_settings", Context.MODE_PRIVATE) }
    var serverUrl by remember { mutableStateOf(prefs.getString("server_url", "ws://10.0.2.2:8000/ws") ?: "ws://10.0.2.2:8000/ws") }

    // State collections
    val connectionStatus by webSocketManager.connectionState.collectAsState()
    val accessPointsMap by markerManager.accessPoints.collectAsState()
    val selectedBssid by markerManager.selectedBssid.collectAsState()
    val calibrationState by calibrationManager.calibrationState.collectAsState()
    val localScans by wifiScanner.scanResults.collectAsState()
    val lastScanTime by wifiScanner.lastScanTimestamp.collectAsState()

    // Dialog sheets
    var showCalibrationDialog by remember { mutableStateOf(false) }
    var showAPListSheet by remember { mutableStateOf(false) }
    var showSettingsDialog by remember { mutableStateOf(false) }
    var showDebugOverlay by remember { mutableStateOf(false) }

    // Camera real-time variables
    var cameraTrackingState by remember { mutableStateOf(TrackingState.PAUSED) }
    var cameraPoseX by remember { mutableFloatStateOf(0f) }
    var cameraPoseY by remember { mutableFloatStateOf(0f) }
    var cameraPoseZ by remember { mutableFloatStateOf(0f) }
    var cameraQx by remember { mutableFloatStateOf(0f) }
    var cameraQy by remember { mutableFloatStateOf(0f) }
    var cameraQz by remember { mutableFloatStateOf(0f) }
    var cameraQw by remember { mutableFloatStateOf(1f) }

    var viewMatrix by remember { mutableStateOf(FloatArray(16)) }
    var projMatrix by remember { mutableStateOf(FloatArray(16)) }

    // SceneView hooks
    val engine = rememberEngine()
    val modelLoader = rememberModelLoader(engine)
    val childNodes = remember { mutableStateListOf<Node>() }

    // Auto-dispatch incoming server AP updates into ARMarkerManager
    LaunchedEffect(Unit) {
        webSocketManager.apUpdates.collectLatest { apUpdate ->
            markerManager.onAPUpdateReceived(apUpdate)
        }
    }

    // Auto-update marker manager with local Wi-Fi scans and upload to server
    LaunchedEffect(localScans) {
        if (localScans.isNotEmpty()) {
            markerManager.onLocalWifiScanResults(localScans)
            // Send mobile Wi-Fi observations + calibrated server pose to server
            val serverPose = transformer.arToServer(cameraPoseX, cameraPoseY, cameraPoseZ)
            webSocketManager.sendWifiObservations(localScans, serverPose)
        }
    }

    Box(modifier = Modifier.fillMaxSize()) {
        // --- 1. FULLSCREEN AR CAMERA SCENE ---
        ARScene(
            modifier = Modifier.fillMaxSize(),
            engine = engine,
            modelLoader = modelLoader,
            childNodes = childNodes,
            onSessionUpdated = { _, frame ->
                val camera = frame.camera
                val state = camera.trackingState
                cameraTrackingState = state

                if (state == TrackingState.TRACKING) {
                    val pose = camera.pose
                    cameraPoseX = pose.tx()
                    cameraPoseY = pose.ty()
                    cameraPoseZ = pose.tz()
                    val q = pose.rotationQuaternion
                    cameraQx = q[0]
                    cameraQy = q[1]
                    cameraQz = q[2]
                    cameraQw = q[3]

                    // Capture projection and view matrices for 3D->2D billboard projection
                    val vm = FloatArray(16)
                    camera.getViewMatrix(vm, 0)
                    viewMatrix = vm

                    val pm = FloatArray(16)
                    camera.getProjectionMatrix(pm, 0, 0.1f, 100.0f)
                    projMatrix = pm

                    // Auto-anchor AR world origin on first tracking frame if not yet calibrated
                    if (!calibrationState.isCalibrated) {
                        calibrationManager.setAnchorCalibration(
                            anchorId = "ORIGIN",
                            serverX = 0.0,
                            serverY = 0.0,
                            serverZ = 0.0,
                            cameraArX = cameraPoseX,
                            cameraArY = cameraPoseY,
                            cameraArZ = cameraPoseZ,
                            qx = cameraQx,
                            qy = cameraQy,
                            qz = cameraQz,
                            qw = cameraQw
                        )
                        markerManager.recalculateArCoordinates()
                    }

                    // Update distances from camera to estimated APs
                    markerManager.updateCameraDistances(cameraPoseX, cameraPoseY, cameraPoseZ)

                    // Dispatch 10 Hz pose to server
                    webSocketManager.sendPose(
                        cameraPoseX, cameraPoseY, cameraPoseZ,
                        cameraQx, cameraQy, cameraQz, cameraQw,
                        state.name
                    )
                }
            }
        )

        // --- 2. AR PREDICTED LOCATION MARKERS & UNCERTAINTY REGIONS ---
        val localizedAPs = accessPointsMap.values.filter { it.isLocalized && it.arPositionX != null }
        val selectedAP = accessPointsMap[selectedBssid]

        for (ap in localizedAPs) {
            val arX = ap.arPositionX ?: continue
            val arY = ap.arPositionY ?: continue
            val arZ = ap.arPositionZ ?: continue

            val screenPoint = markerManager.projectToScreen(
                posX = arX,
                posY = arY,
                posZ = arZ,
                viewMatrix = viewMatrix,
                projMatrix = projMatrix,
                screenWidth = screenWidthPx,
                screenHeight = screenHeightPx
            )

            if (screenPoint.isVisibleInFov) {
                // Render on-screen AR Marker + Predicted Location Area
                APLocationMarkerOverlay(
                    screenPoint = screenPoint,
                    ap = ap,
                    onSelect = { markerManager.selectAP(ap.bssid) }
                )
            } else if (ap.bssid == selectedBssid) {
                // Off-screen Directional Indicator Arrow for selected AP
                OffScreenDirectionIndicator(
                    screenPoint = screenPoint,
                    ap = ap,
                    onClick = { /* selected */ }
                )
            }
        }

        // --- 3. TOP STATUS HUD ---
        TopStatusBar(
            connectionStatus = connectionStatus,
            lastScanTimeMs = lastScanTime,
            apCount = accessPointsMap.size,
            localizedCount = localizedAPs.size,
            isCalibrated = calibrationState.isCalibrated,
            onOpenSettings = { showSettingsDialog = true },
            onOpenCalibration = { showCalibrationDialog = true },
            onOpenAPList = { showAPListSheet = true },
            onToggleDebug = { showDebugOverlay = !showDebugOverlay }
        )

        // --- 4. APPROACH MODE BANNER ---
        if (selectedAP != null) {
            Box(
                modifier = Modifier
                    .align(Alignment.BottomCenter)
                    .padding(bottom = 24.dp)
            ) {
                ApproachModeBanner(
                    ap = selectedAP,
                    onDismiss = { markerManager.selectAP(null) }
                )
            }
        }

        // --- 5. DEBUG OVERLAY ---
        if (showDebugOverlay) {
            DebugInfoOverlay(
                trackingState = cameraTrackingState.name,
                camX = cameraPoseX,
                camY = cameraPoseY,
                camZ = cameraPoseZ,
                serverPose = transformer.arToServer(cameraPoseX, cameraPoseY, cameraPoseZ),
                calibration = calibrationState,
                connection = connectionStatus,
                apCount = accessPointsMap.size,
                localizedCount = localizedAPs.size,
                scanCount = localScans.size,
                modifier = Modifier
                    .align(Alignment.TopStart)
                    .padding(top = 80.dp, start = 16.dp)
            )
        }

        // --- 6. MODALS & SHEETS ---
        if (showSettingsDialog) {
            ServerSettingsDialog(
                currentUrl = serverUrl,
                status = connectionStatus,
                onConnect = { newUrl ->
                    serverUrl = newUrl
                    prefs.edit().putString("server_url", newUrl).apply()
                    webSocketManager.connect(newUrl)
                },
                onDisconnect = { webSocketManager.disconnect() },
                onDismiss = { showSettingsDialog = false }
            )
        }

        if (showCalibrationDialog) {
            CalibrationDialog(
                calibration = calibrationState,
                onCalibrateHere = { anchorId, sX, sY, sZ ->
                    calibrationManager.setAnchorCalibration(
                        anchorId = anchorId,
                        serverX = sX,
                        serverY = sY,
                        serverZ = sZ,
                        cameraArX = cameraPoseX,
                        cameraArY = cameraPoseY,
                        cameraArZ = cameraPoseZ,
                        qx = cameraQx,
                        qy = cameraQy,
                        qz = cameraQz,
                        qw = cameraQw
                    )
                    markerManager.recalculateArCoordinates()
                    showCalibrationDialog = false
                },
                onReset = {
                    calibrationManager.resetCalibration()
                    markerManager.recalculateArCoordinates()
                    showCalibrationDialog = false
                },
                onDismiss = { showCalibrationDialog = false }
            )
        }

        if (showAPListSheet) {
            APListBottomSheet(
                aps = accessPointsMap.values.toList(),
                selectedBssid = selectedBssid,
                onSelectAP = { bssid ->
                    markerManager.selectAP(bssid)
                    showAPListSheet = false
                },
                onDismiss = { showAPListSheet = false }
            )
        }
    }
}

// ==========================================
// AR Location Marker & Uncertainty Area
// ==========================================

@Composable
fun APLocationMarkerOverlay(
    screenPoint: ScreenPoint,
    ap: AccessPointUIState,
    onSelect: () -> Unit
) {
    val density = LocalDensity.current
    val xDp = with(density) { screenPoint.screenX.toDp() }
    val yDp = with(density) { screenPoint.screenY.toDp() }

    // Uncertainty radius in pixels scaled inversely with distance
    val dist = ap.distanceToUserM ?: 5.0f
    val errorM = ap.errorRadiusM.toFloat().coerceAtLeast(0.5f)
    val errorPxRadius = (errorM / dist.coerceAtLeast(1.0f) * 160f).coerceIn(24f, 180f)
    val errorDpRadius = with(density) { errorPxRadius.toDp() }

    Box(
        modifier = Modifier
            .offset { IntOffset((screenPoint.screenX).roundToInt(), (screenPoint.screenY).roundToInt()) }
    ) {
        // --- Uncertainty Circle / Predicted Location Area ---
        Box(
            modifier = Modifier
                .size(errorDpRadius * 2)
                .offset(-errorDpRadius, -errorDpRadius)
                .background(
                    color = when {
                        ap.confidence >= 0.75 -> Color(0x334CAF50)
                        ap.confidence >= 0.45 -> Color(0x33FF9800)
                        else -> Color(0x33F44336)
                    },
                    shape = CircleShape
                )
                .border(
                    width = 2.dp,
                    color = when {
                        ap.confidence >= 0.75 -> Color(0x994CAF50)
                        ap.confidence >= 0.45 -> Color(0x99FF9800)
                        else -> Color(0x99F44336)
                    },
                    shape = CircleShape
                )
        )

        // --- Center AP Pin Point ---
        Box(
            modifier = Modifier
                .size(14.dp)
                .offset((-7).dp, (-7).dp)
                .background(Color.White, CircleShape)
                .border(3.dp, if (ap.isSelected) Color.Yellow else Color(0xFF00E5FF), CircleShape)
        )

        // --- Floating Billboard HUD Card ---
        Card(
            shape = RoundedCornerShape(10.dp),
            colors = CardDefaults.cardColors(
                containerColor = if (ap.isSelected) Color(0xDD002244) else Color(0xDD111827)
            ),
            border = if (ap.isSelected) borderCardStroke(Color.Yellow) else borderCardStroke(Color(0x55FFFFFF)),
            modifier = Modifier
                .offset(x = (-80).dp, y = (-125).dp)
                .width(170.dp)
                .clickable { onSelect() }
        ) {
            Column(modifier = Modifier.padding(8.dp)) {
                // SSID
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    modifier = Modifier.fillMaxWidth()
                ) {
                    Text(
                        text = "📶",
                        fontSize = 14.sp
                    )
                    Spacer(modifier = Modifier.width(4.dp))
                    Text(
                        text = ap.ssid,
                        style = MaterialTheme.typography.titleSmall,
                        fontWeight = FontWeight.Bold,
                        color = Color.White,
                        maxLines = 1
                    )
                }

                Spacer(modifier = Modifier.height(3.dp))

                // Real Phone RSSI
                val phoneRssi = ap.phoneRssiDbm
                Text(
                    text = if (phoneRssi != null) "● $phoneRssi dBm (phone)" else "● No local signal",
                    style = MaterialTheme.typography.labelSmall,
                    color = if (phoneRssi != null && phoneRssi > -65) Color(0xFF4CAF50) else Color(0xFFFFB74D),
                    fontWeight = FontWeight.SemiBold
                )

                // Distance
                Text(
                    text = ap.distanceToUserM?.let { "~%.1f m away".format(it) } ?: "Estimating distance...",
                    style = MaterialTheme.typography.labelSmall,
                    color = Color.LightGray
                )

                // Confidence & Accuracy
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.SpaceBetween
                ) {
                    Text(
                        text = "Conf: ${(ap.confidence * 100).roundToInt()}%",
                        style = MaterialTheme.typography.labelSmall,
                        color = Color(0xFF81D4FA)
                    )
                    Text(
                        text = "±%.1fm".format(ap.errorRadiusM),
                        style = MaterialTheme.typography.labelSmall,
                        color = Color(0xFFB0BEC5)
                    )
                }
            }
        }
    }
}

private fun borderCardStroke(color: Color) = androidx.compose.foundation.BorderStroke(1.5.dp, color)

// ==========================================
// Off-Screen Directional Indicator
// ==========================================

@Composable
fun OffScreenDirectionIndicator(
    screenPoint: ScreenPoint,
    ap: AccessPointUIState,
    onClick: () -> Unit
) {
    val density = LocalDensity.current
    val x = screenPoint.screenX
    val y = screenPoint.screenY

    Box(
        modifier = Modifier
            .offset { IntOffset((x - 45).roundToInt(), (y - 25).roundToInt()) }
            .background(Color(0xDDFF9800), RoundedCornerShape(16.dp))
            .clickable { onClick() }
            .padding(horizontal = 10.dp, vertical = 6.dp)
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = "➤",
                fontSize = 13.sp,
                fontWeight = FontWeight.Bold,
                color = Color.Black,
                modifier = Modifier.rotate(screenPoint.edgeAngleDegrees)
            )
            Spacer(modifier = Modifier.width(4.dp))
            Text(
                text = "${ap.ssid} (${ap.distanceToUserM?.let { "%.1fm".format(it) } ?: "..."})",
                style = MaterialTheme.typography.labelMedium,
                fontWeight = FontWeight.Bold,
                color = Color.Black
            )
        }
    }
}

// ==========================================
// Top Status Bar
// ==========================================

@Composable
fun TopStatusBar(
    connectionStatus: ConnectionStatus,
    lastScanTimeMs: Long,
    apCount: Int,
    localizedCount: Int,
    isCalibrated: Boolean,
    onOpenSettings: () -> Unit,
    onOpenCalibration: () -> Unit,
    onOpenAPList: () -> Unit,
    onToggleDebug: () -> Unit
) {
    val scanAgeSec = if (lastScanTimeMs > 0) ((System.currentTimeMillis() - lastScanTimeMs) / 1000L).coerceAtLeast(0) else -1

    Card(
        shape = RoundedCornerShape(16.dp),
        colors = CardDefaults.cardColors(containerColor = Color(0xCC000000)),
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 12.dp, vertical = 8.dp)
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 12.dp, vertical = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.SpaceBetween
        ) {
            // Server connection badge
            Row(verticalAlignment = Alignment.CenterVertically) {
                Box(
                    modifier = Modifier
                        .size(10.dp)
                        .background(
                            color = when (connectionStatus) {
                                ConnectionStatus.CONNECTED -> Color(0xFF4CAF50)
                                ConnectionStatus.CONNECTING, ConnectionStatus.RECONNECTING -> Color(0xFFFF9800)
                                else -> Color(0xFFF44336)
                            },
                            shape = CircleShape
                        )
                )
                Spacer(modifier = Modifier.width(6.dp))
                Text(
                    text = connectionStatus.name,
                    style = MaterialTheme.typography.labelMedium,
                    fontWeight = FontWeight.Bold,
                    color = Color.White
                )
            }

            // AP Counts & Scan Age
            Text(
                text = if (scanAgeSec >= 0) "APs: $localizedCount/$apCount (${scanAgeSec}s)" else "No Wi-Fi scan",
                style = MaterialTheme.typography.labelSmall,
                color = Color.LightGray
            )

            // Buttons
            Row(verticalAlignment = Alignment.CenterVertically) {
                IconButton(onClick = onOpenCalibration, modifier = Modifier.size(32.dp)) {
                    Text(text = "🧭", fontSize = 18.sp)
                }
                IconButton(onClick = onOpenAPList, modifier = Modifier.size(32.dp)) {
                    Text(text = "📋", fontSize = 18.sp)
                }
                IconButton(onClick = onOpenSettings, modifier = Modifier.size(32.dp)) {
                    Text(text = "⚙️", fontSize = 18.sp)
                }
                IconButton(onClick = onToggleDebug, modifier = Modifier.size(32.dp)) {
                    Text(text = "🐞", fontSize = 18.sp)
                }
            }
        }
    }
}

// ==========================================
// Approach Mode Banner
// ==========================================

@Composable
fun ApproachModeBanner(
    ap: AccessPointUIState,
    onDismiss: () -> Unit
) {
    Card(
        shape = RoundedCornerShape(16.dp),
        colors = CardDefaults.cardColors(containerColor = Color(0xEE0B192C)),
        border = borderCardStroke(Color(0xFF00E5FF)),
        modifier = Modifier
            .fillMaxWidth(0.92f)
            .padding(8.dp)
    ) {
        Column(modifier = Modifier.padding(14.dp)) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically
            ) {
                Column {
                    Text(
                        text = "APPROACHING: ${ap.ssid}",
                        style = MaterialTheme.typography.titleMedium,
                        fontWeight = FontWeight.Bold,
                        color = Color(0xFF00E5FF)
                    )
                    Text(
                        text = ap.bssid,
                        style = MaterialTheme.typography.labelSmall,
                        fontFamily = FontFamily.Monospace,
                        color = Color.LightGray
                    )
                }
                IconButton(onClick = onDismiss) {
                    Icon(imageVector = Icons.Default.Close, contentDescription = "Close", tint = Color.White)
                }
            }

            Spacer(modifier = Modifier.height(8.dp))

            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween
            ) {
                Column {
                    Text(text = "PHONE RSSI", style = MaterialTheme.typography.labelSmall, color = Color.Gray)
                    Text(
                        text = ap.phoneRssiDbm?.let { "$it dBm" } ?: "Searching...",
                        style = MaterialTheme.typography.headlineSmall,
                        fontWeight = FontWeight.Bold,
                        color = Color(0xFF4CAF50)
                    )
                }
                Column(horizontalAlignment = Alignment.End) {
                    Text(text = "ESTIMATED DISTANCE", style = MaterialTheme.typography.labelSmall, color = Color.Gray)
                    Text(
                        text = ap.distanceToUserM?.let { "~%.1f m".format(it) } ?: "-- m",
                        style = MaterialTheme.typography.headlineSmall,
                        fontWeight = FontWeight.Bold,
                        color = Color.White
                    )
                }
            }

            Spacer(modifier = Modifier.height(6.dp))

            // Trend indicator
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    text = if (ap.rssiTrend.contains("stronger")) "▲" else "▶",
                    color = if (ap.rssiTrend.contains("stronger")) Color(0xFF4CAF50) else Color(0xFFFFB74D),
                    fontWeight = FontWeight.Bold,
                    fontSize = 14.sp
                )
                Spacer(modifier = Modifier.width(6.dp))
                Text(
                    text = ap.rssiTrend,
                    style = MaterialTheme.typography.bodySmall,
                    color = Color.White,
                    fontWeight = FontWeight.Medium
                )
            }
        }
    }
}

// ==========================================
// Debug Overlay
// ==========================================

@Composable
fun DebugInfoOverlay(
    trackingState: String,
    camX: Float,
    camY: Float,
    camZ: Float,
    serverPose: APPosition,
    calibration: CalibrationState,
    connection: ConnectionStatus,
    apCount: Int,
    localizedCount: Int,
    scanCount: Int,
    modifier: Modifier = Modifier
) {
    Card(
        shape = RoundedCornerShape(8.dp),
        colors = CardDefaults.cardColors(containerColor = Color(0xDD000000)),
        modifier = modifier.width(280.dp)
    ) {
        Column(modifier = Modifier.padding(10.dp)) {
            Text(
                text = "TELEMETRY DEBUG",
                style = MaterialTheme.typography.labelMedium,
                fontWeight = FontWeight.Bold,
                color = Color.Yellow
            )
            Spacer(modifier = Modifier.height(4.dp))
            Text(
                text = "ARCore: $trackingState\n" +
                        "AR Pose: (%.2f, %.2f, %.2f)\n".format(camX, camY, camZ) +
                        "Server Pose: (%.2f, %.2f, %.2f)\n".format(serverPose.x, serverPose.y, serverPose.z) +
                        "Calibrated: ${calibration.isCalibrated} (Yaw: %.1f°)\n".format(calibration.rotationYawDegrees) +
                        "Server WS: ${connection.name}\n" +
                        "Raw Scans: $scanCount | APs: $localizedCount/$apCount",
                style = MaterialTheme.typography.bodySmall,
                fontFamily = FontFamily.Monospace,
                color = Color(0xFF00FF66),
                fontSize = 11.sp
            )
        }
    }
}

// ==========================================
// Calibration Dialog
// ==========================================

@Composable
fun CalibrationDialog(
    calibration: CalibrationState,
    onCalibrateHere: (anchorId: String, sX: Double, sY: Double, sZ: Double) -> Unit,
    onReset: () -> Unit,
    onDismiss: () -> Unit
) {
    var anchorId by remember { mutableStateOf("ANCHOR-01") }
    var serverX by remember { mutableStateOf("0.0") }
    var serverY by remember { mutableStateOf("0.0") }
    var serverZ by remember { mutableStateOf("0.0") }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Calibrate AR World") },
        text = {
            Column {
                Text(
                    text = "Stand at a known reference point (anchor) in the room and face North/Forward. Then tap 'Set AR Origin Here'.",
                    style = MaterialTheme.typography.bodyMedium
                )
                Spacer(modifier = Modifier.height(12.dp))
                OutlinedTextField(
                    value = anchorId,
                    onValueChange = { anchorId = it },
                    label = { Text("Anchor ID") },
                    modifier = Modifier.fillMaxWidth()
                )
                Spacer(modifier = Modifier.height(8.dp))
                Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    OutlinedTextField(
                        value = serverX,
                        onValueChange = { serverX = it },
                        label = { Text("X (m)") },
                        modifier = Modifier.weight(1f)
                    )
                    OutlinedTextField(
                        value = serverY,
                        onValueChange = { serverY = it },
                        label = { Text("Y (m)") },
                        modifier = Modifier.weight(1f)
                    )
                    OutlinedTextField(
                        value = serverZ,
                        onValueChange = { serverZ = it },
                        label = { Text("Z (m)") },
                        modifier = Modifier.weight(1f)
                    )
                }
                Spacer(modifier = Modifier.height(12.dp))
                Text(
                    text = if (calibration.isCalibrated) "Current status: CALIBRATED (Anchor: ${calibration.anchorId})" else "Current status: NOT CALIBRATED",
                    style = MaterialTheme.typography.labelMedium,
                    color = if (calibration.isCalibrated) Color(0xFF4CAF50) else Color(0xFFFF9800)
                )
            }
        },
        confirmButton = {
            Button(onClick = {
                val x = serverX.toDoubleOrNull() ?: 0.0
                val y = serverY.toDoubleOrNull() ?: 0.0
                val z = serverZ.toDoubleOrNull() ?: 0.0
                onCalibrateHere(anchorId, x, y, z)
            }) {
                Text("Set AR Origin Here")
            }
        },
        dismissButton = {
            Row {
                if (calibration.isCalibrated) {
                    TextButton(onClick = onReset) {
                        Text("Reset Alignment", color = Color.Red)
                    }
                }
                TextButton(onClick = onDismiss) {
                    Text("Cancel")
                }
            }
        }
    )
}

// ==========================================
// Server Settings Dialog
// ==========================================

@Composable
fun ServerSettingsDialog(
    currentUrl: String,
    status: ConnectionStatus,
    onConnect: (String) -> Unit,
    onDisconnect: () -> Unit,
    onDismiss: () -> Unit
) {
    var urlInput by remember { mutableStateOf(currentUrl) }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Server Configuration") },
        text = {
            Column {
                Text("Enter the Go server WebSocket endpoint (e.g. ws://192.168.1.50:8000/ws):")
                Spacer(modifier = Modifier.height(8.dp))
                OutlinedTextField(
                    value = urlInput,
                    onValueChange = { urlInput = it },
                    label = { Text("Server WebSocket URL") },
                    modifier = Modifier.fillMaxWidth()
                )
                Spacer(modifier = Modifier.height(8.dp))
                Text(
                    text = "Connection: ${status.name}",
                    style = MaterialTheme.typography.labelMedium,
                    fontWeight = FontWeight.Bold,
                    color = if (status == ConnectionStatus.CONNECTED) Color(0xFF4CAF50) else Color(0xFFF44336)
                )
            }
        },
        confirmButton = {
            Button(onClick = {
                onConnect(urlInput)
                onDismiss()
            }) {
                Text("Connect")
            }
        },
        dismissButton = {
            Row {
                if (status == ConnectionStatus.CONNECTED) {
                    TextButton(onClick = {
                        onDisconnect()
                        onDismiss()
                    }) {
                        Text("Disconnect", color = Color.Red)
                    }
                }
                TextButton(onClick = onDismiss) {
                    Text("Close")
                }
            }
        }
    )
}

// ==========================================
// AP List Modal Sheet
// ==========================================

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun APListBottomSheet(
    aps: List<AccessPointUIState>,
    selectedBssid: String?,
    onSelectAP: (String) -> Unit,
    onDismiss: () -> Unit
) {
    var filterTab by remember { mutableIntStateOf(0) }
    val filteredList = remember(aps, filterTab) {
        when (filterTab) {
            1 -> aps.filter { it.isLocalized }
            2 -> aps.filter { (it.phoneRssiDbm ?: -100) >= -65 }
            3 -> aps.filter { it.confidence >= 0.7 }
            else -> aps
        }.sortedByDescending { it.phoneRssiDbm ?: -999 }
    }

    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(16.dp)
        ) {
            Text(
                text = "Detected Wi-Fi Access Points (${filteredList.size})",
                style = MaterialTheme.typography.titleMedium,
                fontWeight = FontWeight.Bold
            )
            Spacer(modifier = Modifier.height(8.dp))

            TabRow(selectedTabIndex = filterTab) {
                Tab(selected = filterTab == 0, onClick = { filterTab = 0 }, text = { Text("All") })
                Tab(selected = filterTab == 1, onClick = { filterTab = 1 }, text = { Text("Localized") })
                Tab(selected = filterTab == 2, onClick = { filterTab = 2 }, text = { Text("Strong") })
                Tab(selected = filterTab == 3, onClick = { filterTab = 3 }, text = { Text("High Conf") })
            }

            Spacer(modifier = Modifier.height(8.dp))

            LazyColumn(modifier = Modifier.fillMaxHeight(0.6f)) {
                items(filteredList) { ap ->
                    Card(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(vertical = 4.dp)
                            .clickable { onSelectAP(ap.bssid) },
                        colors = CardDefaults.cardColors(
                            containerColor = if (ap.bssid == selectedBssid) MaterialTheme.colorScheme.primaryContainer else MaterialTheme.colorScheme.surfaceVariant
                        )
                    ) {
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .padding(12.dp),
                            horizontalArrangement = Arrangement.SpaceBetween,
                            verticalAlignment = Alignment.CenterVertically
                        ) {
                            Column {
                                Text(
                                    text = ap.ssid,
                                    style = MaterialTheme.typography.titleSmall,
                                    fontWeight = FontWeight.Bold
                                )
                                Text(
                                    text = ap.bssid,
                                    style = MaterialTheme.typography.bodySmall,
                                    fontFamily = FontFamily.Monospace,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant
                                )
                                if (ap.isLocalized) {
                                    Text(
                                        text = "Conf: ${(ap.confidence * 100).roundToInt()}% | ±%.1fm | %d hubs".format(ap.errorRadiusM, ap.hubCount),
                                        style = MaterialTheme.typography.labelSmall,
                                        color = Color(0xFF0288D1)
                                    )
                                }
                            }

                            Column(horizontalAlignment = Alignment.End) {
                                Text(
                                    text = ap.phoneRssiDbm?.let { "$it dBm" } ?: "No signal",
                                    style = MaterialTheme.typography.bodyMedium,
                                    fontWeight = FontWeight.Bold,
                                    color = if ((ap.phoneRssiDbm ?: -100) > -65) Color(0xFF388E3C) else Color(0xFFF57C00)
                                )
                                if (ap.distanceToUserM != null) {
                                    Text(
                                        text = "~%.1f m".format(ap.distanceToUserM),
                                        style = MaterialTheme.typography.labelSmall,
                                        color = MaterialTheme.colorScheme.onSurfaceVariant
                                    )
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}