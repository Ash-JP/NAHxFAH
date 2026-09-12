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
import androidx.compose.ui.graphics.toArgb
import io.github.sceneview.ar.ARScene
import io.github.sceneview.node.Node
import io.github.sceneview.node.SphereNode
import io.github.sceneview.rememberEngine
import io.github.sceneview.rememberMaterialLoader
import io.github.sceneview.rememberModelLoader
import io.github.sceneview.math.Position
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.launch
import kotlin.math.roundToInt
import kotlin.math.atan2
import kotlin.math.abs
import kotlin.math.sqrt

class MainActivity : ComponentActivity() {

    private lateinit var wifiScanner: WifiScanner
    private lateinit var transformer: CoordinateTransformer
    private lateinit var calibrationManager: CalibrationManager
    private lateinit var webSocketManager: ServerWebSocketManager
    private lateinit var markerManager: ARMarkerManager

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        // Prevent crashes from uncaught background worker exceptions
        val defaultHandler = Thread.getDefaultUncaughtExceptionHandler()
        Thread.setDefaultUncaughtExceptionHandler { thread, throwable ->
            android.util.Log.e("WIFI_HUNTER_CRASH", "Uncaught exception on thread ${thread.name}", throwable)
            defaultHandler?.uncaughtException(thread, throwable)
        }

        transformer = CoordinateTransformer()
        calibrationManager = CalibrationManager(this, transformer)
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
    val hubsMap by markerManager.hubs.collectAsState()
    val selectedHubId by markerManager.selectedHubId.collectAsState()
    val calibrationState by calibrationManager.calibrationState.collectAsState()
    val localScans by wifiScanner.scanResults.collectAsState()
    val lastScanTime by wifiScanner.lastScanTimestamp.collectAsState()

    // Dialog sheets
    var showCalibrationDialog by remember { mutableStateOf(false) }
    var showHubsSheet by remember { mutableStateOf(false) }
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
    val materialLoader = rememberMaterialLoader(engine)
    val childNodes = remember { mutableStateListOf<Node>() }

    // Pose history buffer for timestamp correlation (Section 30, 31)
    val poseHistory = remember { PoseHistoryBuffer(maxDurationMs = 10_000L) }

    // Movement-based spatial observation tracking
    var lastDispatchX by remember { mutableFloatStateOf(0f) }
    var lastDispatchY by remember { mutableFloatStateOf(0f) }
    var lastDispatchZ by remember { mutableFloatStateOf(0f) }
    var lastDispatchTimeMs by remember { mutableLongStateOf(0L) }

    // Auto-dispatch incoming server AP and Hub updates into ARMarkerManager
    LaunchedEffect(Unit) {
        launch {
            webSocketManager.apUpdates.collectLatest { apUpdate ->
                markerManager.onAPUpdateReceived(apUpdate)
            }
        }
        launch {
            webSocketManager.hubsState.collectLatest { hubs ->
                markerManager.updateHubs(hubs)
            }
        }
    }

    // Auto-update marker manager with local Wi-Fi scans and upload to server
    LaunchedEffect(localScans) {
        if (localScans.isNotEmpty()) {
            markerManager.onLocalWifiScanResults(localScans)
            if (cameraTrackingState == TrackingState.TRACKING) {
                val serverPose = transformer.arToServer(cameraPoseX, cameraPoseY, cameraPoseZ)
                webSocketManager.sendWifiObservations(localScans, serverPose)
                lastDispatchX = cameraPoseX
                lastDispatchY = cameraPoseY
                lastDispatchZ = cameraPoseZ
                lastDispatchTimeMs = System.currentTimeMillis()
            }
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
                try {
                    val camera = frame.camera ?: return@ARScene
                    val state = camera.trackingState
                    cameraTrackingState = state

                    if (state == TrackingState.TRACKING) {
                        val pose = camera.pose ?: return@ARScene
                        cameraPoseX = pose.tx()
                        cameraPoseY = pose.ty()
                        cameraPoseZ = pose.tz()
                        val q = pose.rotationQuaternion
                        if (q.size >= 4) {
                            cameraQx = q[0]
                            cameraQy = q[1]
                            cameraQz = q[2]
                            cameraQw = q[3]
                        }

                        // Record to pose history buffer for timestamp correlation
                        poseHistory.addPose(
                            TimestampedPose(
                                timestampMs = System.currentTimeMillis(),
                                x = cameraPoseX,
                                y = cameraPoseY,
                                z = cameraPoseZ,
                                qx = cameraQx,
                                qy = cameraQy,
                                qz = cameraQz,
                                qw = cameraQw,
                                isTracking = true
                            )
                        )

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

                        // Continuous observation dispatching as user moves through space
                        if (localScans.isNotEmpty()) {
                            val dx = cameraPoseX - lastDispatchX
                            val dy = cameraPoseY - lastDispatchY
                            val dz = cameraPoseZ - lastDispatchZ
                            val distMoved = sqrt(dx * dx + dy * dy + dz * dz)
                            val now = System.currentTimeMillis()
                            val elapsedMs = now - lastDispatchTimeMs

                            if (lastDispatchTimeMs == 0L || distMoved >= 0.5f || (elapsedMs >= 3000L && distMoved >= 0.15f)) {
                                lastDispatchX = cameraPoseX
                                lastDispatchY = cameraPoseY
                                lastDispatchZ = cameraPoseZ
                                lastDispatchTimeMs = now

                                val serverPose = transformer.arToServer(cameraPoseX, cameraPoseY, cameraPoseZ)
                                webSocketManager.sendWifiObservations(localScans, serverPose)
                                markerManager.onLocalWifiScanResults(localScans)
                            }
                        }

                        // Dispatch 10 Hz pose to server
                        webSocketManager.sendPose(
                            cameraPoseX, cameraPoseY, cameraPoseZ,
                            cameraQx, cameraQy, cameraQz, cameraQw,
                            state.name
                        )
                    } else {
                        // Pause pose history when tracking is lost, but preserve calibration!
                        poseHistory.clear()
                        lastDispatchTimeMs = 0L
                    }
                } catch (t: Throwable) {
                    android.util.Log.e("MainARScreen", "Error during AR frame update", t)
                }
            }
        )

        // --- 2. AR PREDICTED LOCATION MARKERS & UNCERTAINTY REGIONS ---
        val activeAPs = accessPointsMap.values
            .filter { it.isLocalized && it.hasSpatialPosition }
            .sortedByDescending { it.phoneRssiDbm ?: -999 }
            .take(12)
        val selectedAP = accessPointsMap[selectedBssid]

        val calibratedHubs = hubsMap.values
            .filter { it.isCalibrated && it.arPositionX != null && it.arPositionY != null && it.arPositionZ != null }
        val selectedHub = hubsMap[selectedHubId]

        // Safely manage 3D glowing spheres without memory leaks or render collisions
        val nodeMap = remember { mutableMapOf<String, SphereNode>() }

        DisposableEffect(Unit) {
            onDispose {
                try {
                    nodeMap.values.forEach { it.destroy() }
                    nodeMap.clear()
                    childNodes.clear()
                } catch (t: Throwable) {
                    android.util.Log.e("MainARScreen", "Error disposing AR scene nodes", t)
                }
            }
        }

        LaunchedEffect(activeAPs, calibratedHubs) {
            try {
                val currentKeys = mutableSetOf<String>()

                // 1. Localized Access Points (cyan/amber/red based on confidence)
                for (ap in activeAPs) {
                    val key = "ap_${ap.bssid}"
                    currentKeys.add(key)
                    val arX = ap.arPositionX ?: continue
                    val arY = ap.arPositionY ?: continue
                    val arZ = ap.arPositionZ ?: continue

                    val existingNode = nodeMap[key]
                    if (existingNode != null) {
                        existingNode.position = Position(arX, arY, arZ)
                    } else {
                        val color = when {
                            ap.confidence >= 0.75 -> Color(0xFF00E5FF).toArgb()
                            ap.confidence >= 0.45 -> Color(0xFFFFB300).toArgb()
                            else -> Color(0xFFFF5252).toArgb()
                        }
                        val mat = materialLoader.createColorInstance(color)
                        val sphere = SphereNode(
                            engine = engine,
                            radius = 0.15f,
                            materialInstance = mat
                        )
                        sphere.position = Position(arX, arY, arZ)
                        nodeMap[key] = sphere
                        childNodes.add(sphere)
                    }
                }

                // 2. Calibrated Venue Hubs (purple/violet beacons)
                for (hub in calibratedHubs) {
                    val key = "hub_${hub.hubId}"
                    currentKeys.add(key)
                    val arX = hub.arPositionX ?: continue
                    val arY = hub.arPositionY ?: continue
                    val arZ = hub.arPositionZ ?: continue

                    val existingNode = nodeMap[key]
                    if (existingNode != null) {
                        existingNode.position = Position(arX, arY, arZ)
                    } else {
                        val mat = materialLoader.createColorInstance(Color(0xFFBB86FC).toArgb())
                        val sphere = SphereNode(
                            engine = engine,
                            radius = 0.20f,
                            materialInstance = mat
                        )
                        sphere.position = Position(arX, arY, arZ)
                        nodeMap[key] = sphere
                        childNodes.add(sphere)
                    }
                }

                // Clean up removed nodes so Filament memory is never exhausted
                val it = nodeMap.iterator()
                while (it.hasNext()) {
                    val entry = it.next()
                    if (entry.key !in currentKeys) {
                        try {
                            entry.value.destroy()
                        } catch (_: Throwable) {}
                        childNodes.remove(entry.value)
                        it.remove()
                    }
                }
            } catch (t: Throwable) {
                android.util.Log.e("MainARScreen", "Error updating AR scene nodes", t)
            }
        }

        // 2D overlays for Access Points
        for (ap in activeAPs) {
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
                APLocationMarkerOverlay(
                    screenPoint = screenPoint,
                    ap = ap,
                    onSelect = { markerManager.selectAP(ap.bssid) }
                )
            } else if (ap.bssid == selectedBssid) {
                OffScreenDirectionIndicator(
                    screenPoint = screenPoint,
                    ap = ap,
                    onClick = { /* selected */ }
                )
            }
        }

        // 2D overlays for Venue Hubs
        for (hub in calibratedHubs) {
            val arX = hub.arPositionX ?: continue
            val arY = hub.arPositionY ?: continue
            val arZ = hub.arPositionZ ?: continue

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
                HubLocationMarkerOverlay(
                    screenPoint = screenPoint,
                    hub = hub,
                    onSelect = { markerManager.selectHub(hub.hubId) }
                )
            } else if (hub.hubId == selectedHubId) {
                HubOffScreenDirectionIndicator(
                    screenPoint = screenPoint,
                    hub = hub,
                    onClick = { markerManager.selectHub(null) }
                )
            }
        }

        // --- 3. TOP STATUS HUD ---
        TopStatusBar(
            connectionStatus = connectionStatus,
            lastScanTimeMs = lastScanTime,
            apCount = accessPointsMap.size,
            localizedCount = activeAPs.size,
            hubsCount = hubsMap.size,
            uncalibratedHubsCount = hubsMap.values.count { !it.isCalibrated },
            isCalibrated = calibrationState.isCalibrated,
            onOpenSettings = { showSettingsDialog = true },
            onOpenCalibration = { showCalibrationDialog = true },
            onOpenHubs = { showHubsSheet = true },
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

        // --- 4b. MOVEMENT GUIDANCE BANNER (Section 32) ---
        if (activeAPs.isEmpty() && localScans.isNotEmpty()) {
            Surface(
                color = Color(0xCC111111),
                shape = RoundedCornerShape(20.dp),
                border = androidx.compose.foundation.BorderStroke(1.dp, Color(0xFF00E5FF).copy(alpha = 0.5f)),
                modifier = Modifier
                    .align(Alignment.BottomCenter)
                    .padding(bottom = 90.dp)
            ) {
                Row(
                    modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    Text(text = "🚶", fontSize = 16.sp)
                    Spacer(modifier = Modifier.width(8.dp))
                    Text(
                        text = "Move around the room to collect spatial Wi-Fi measurements",
                        color = Color.White,
                        style = MaterialTheme.typography.labelMedium,
                        fontWeight = FontWeight.Medium
                    )
                }
            }
        }

        // --- 5. DEBUG OVERLAY ---
        if (showDebugOverlay) {
            DebugInfoOverlay(
                trackingState = cameraTrackingState.name,
                camX = cameraPoseX,
                camY = cameraPoseY,
                camZ = cameraPoseZ,
                qx = cameraQx,
                qy = cameraQy,
                qz = cameraQz,
                qw = cameraQw,
                serverPose = transformer.arToServer(cameraPoseX, cameraPoseY, cameraPoseZ),
                selectedAP = selectedAP,
                calibration = calibrationState,
                connection = connectionStatus,
                apCount = accessPointsMap.size,
                localizedCount = activeAPs.size,
                scanCount = localScans.size,
                modifier = Modifier
                    .align(Alignment.TopStart)
                    .statusBarsPadding()
                    .padding(top = 120.dp, start = 14.dp)
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
                    webSocketManager.sendSaveAnchor(
                        anchorId = anchorId,
                        name = "Room Origin ($anchorId)",
                        x = sX,
                        y = sY,
                        z = sZ
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

        if (showHubsSheet) {
            VenueHubsBottomSheet(
                hubs = hubsMap.values.toList(),
                cameraX = cameraPoseX,
                cameraY = cameraPoseY,
                cameraZ = cameraPoseZ,
                transformer = transformer,
                onAnchorHub = { hubId, sX, sY, sZ ->
                    webSocketManager.sendUpdateHubPosition(hubId, sX, sY, sZ)
                    webSocketManager.sendSaveAnchor(
                        anchorId = hubId,
                        name = "Venue Hub $hubId",
                        x = sX,
                        y = sY,
                        z = sZ
                    )
                    markerManager.recalculateArCoordinates()
                },
                onAlignArToHub = { hub ->
                    calibrationManager.setAnchorCalibration(
                        anchorId = hub.hubId,
                        serverX = hub.serverPositionX,
                        serverY = hub.serverPositionY,
                        serverZ = hub.serverPositionZ,
                        cameraArX = cameraPoseX,
                        cameraArY = cameraPoseY,
                        cameraArZ = cameraPoseZ,
                        qx = cameraQx,
                        qy = cameraQy,
                        qz = cameraQz,
                        qw = cameraQw
                    )
                    markerManager.recalculateArCoordinates()
                    showHubsSheet = false
                },
                onSelectHub = { hubId ->
                    markerManager.selectHub(hubId)
                    showHubsSheet = false
                },
                onDismiss = { showHubsSheet = false }
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
    if (screenPoint.screenX.isNaN() || screenPoint.screenY.isNaN() || screenPoint.screenX < -1000f || screenPoint.screenY < -1000f) return
    val density = LocalDensity.current
    val sx = screenPoint.screenX.roundToInt()
    val sy = screenPoint.screenY.roundToInt()

    // Uncertainty radius in pixels scaled inversely with distance
    val dist = ap.distanceToUserM ?: 5.0f
    val errorM = ap.errorRadiusM.toFloat().coerceAtLeast(0.5f)
    val errorPxRadius = (errorM / dist.coerceAtLeast(1.0f) * 160f).coerceIn(24f, 180f)
    val errorDpRadius = with(density) { errorPxRadius.toDp() }

    Box(
        modifier = Modifier
            .offset { IntOffset(sx, sy) }
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
    if (screenPoint.screenX.isNaN() || screenPoint.screenY.isNaN() || screenPoint.screenX < -1000f || screenPoint.screenY < -1000f) return
    val x = screenPoint.screenX.roundToInt()
    val y = screenPoint.screenY.roundToInt()
    val angle = if (screenPoint.edgeAngleDegrees.isNaN() || screenPoint.edgeAngleDegrees.isInfinite()) 0f else screenPoint.edgeAngleDegrees

    Box(
        modifier = Modifier
            .offset { IntOffset(x - 45, y - 25) }
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
                modifier = Modifier.rotate(angle)
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
    hubsCount: Int,
    uncalibratedHubsCount: Int,
    isCalibrated: Boolean,
    onOpenSettings: () -> Unit,
    onOpenCalibration: () -> Unit,
    onOpenHubs: () -> Unit,
    onOpenAPList: () -> Unit,
    onToggleDebug: () -> Unit
) {
    val scanAgeSec = if (lastScanTimeMs > 0) ((System.currentTimeMillis() - lastScanTimeMs) / 1000L).coerceAtLeast(0) else -1

    Card(
        shape = RoundedCornerShape(16.dp),
        colors = CardDefaults.cardColors(containerColor = Color(0xEE111827)),
        border = borderCardStroke(Color(0x44FFFFFF)),
        modifier = Modifier
            .fillMaxWidth()
            .statusBarsPadding()
            .padding(top = 10.dp, start = 12.dp, end = 12.dp, bottom = 4.dp)
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 12.dp, vertical = 10.dp)
        ) {
            // Row 1: Connection status badge & Telemetry (APs / Hubs / Scan age)
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.SpaceBetween
            ) {
                // Connection badge
                Surface(
                    color = when (connectionStatus) {
                        ConnectionStatus.CONNECTED -> Color(0x334CAF50)
                        ConnectionStatus.CONNECTING, ConnectionStatus.RECONNECTING -> Color(0x33FF9800)
                        else -> Color(0x33F44336)
                    },
                    shape = RoundedCornerShape(8.dp)
                ) {
                    Row(
                        verticalAlignment = Alignment.CenterVertically,
                        modifier = Modifier.padding(horizontal = 8.dp, vertical = 4.dp)
                    ) {
                        Box(
                            modifier = Modifier
                                .size(8.dp)
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
                            text = when (connectionStatus) {
                                ConnectionStatus.CONNECTED -> "ONLINE"
                                ConnectionStatus.CONNECTING -> "CONNECTING"
                                ConnectionStatus.RECONNECTING -> "RETRYING"
                                else -> "OFFLINE"
                            },
                            style = MaterialTheme.typography.labelSmall,
                            fontWeight = FontWeight.Bold,
                            color = Color.White
                        )
                    }
                }

                // AP Counts & Hubs Count
                Text(
                    text = if (scanAgeSec >= 0) "APs: $localizedCount/$apCount • $hubsCount Hubs (${scanAgeSec}s)" else "$hubsCount Hubs Active",
                    style = MaterialTheme.typography.labelSmall,
                    color = Color(0xFFE0E0E0),
                    fontWeight = FontWeight.Medium
                )
            }

            Spacer(modifier = Modifier.height(8.dp))

            // Row 2: Quick Calibration Pill & Action Buttons
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.SpaceBetween
            ) {
                // Calibration Status Pill (Clickable)
                Surface(
                    color = if (isCalibrated) Color(0x3300E5FF) else Color(0x33FF9800),
                    shape = RoundedCornerShape(8.dp),
                    border = borderCardStroke(if (isCalibrated) Color(0x8800E5FF) else Color(0x88FF9800)),
                    modifier = Modifier.clickable { onOpenCalibration() }
                ) {
                    Row(
                        verticalAlignment = Alignment.CenterVertically,
                        modifier = Modifier.padding(horizontal = 8.dp, vertical = 5.dp)
                    ) {
                        Text(
                            text = if (isCalibrated) "🧭 Origin Anchored" else "🧭 Calibrate Origin",
                            fontSize = 11.sp,
                            fontWeight = FontWeight.SemiBold,
                            color = if (isCalibrated) Color(0xFF00E5FF) else Color(0xFFFFB74D)
                        )
                    }
                }

                // Action Buttons
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(6.dp)
                ) {
                    // Hubs Button
                    Box(
                        modifier = Modifier
                            .size(34.dp)
                            .background(Color(0x337C4DFF), RoundedCornerShape(8.dp))
                            .clickable { onOpenHubs() },
                        contentAlignment = Alignment.Center
                    ) {
                        BadgedBox(badge = {
                            if (uncalibratedHubsCount > 0) {
                                Badge(containerColor = Color(0xFFFF9800)) {
                                    Text("$uncalibratedHubsCount", color = Color.White, fontSize = 9.sp)
                                }
                            }
                        }) {
                            Text(text = "💻", fontSize = 16.sp)
                        }
                    }

                    // AP List Button
                    Box(
                        modifier = Modifier
                            .size(34.dp)
                            .background(Color(0x22FFFFFF), RoundedCornerShape(8.dp))
                            .clickable { onOpenAPList() },
                        contentAlignment = Alignment.Center
                    ) {
                        Text(text = "📋", fontSize = 16.sp)
                    }

                    // Settings Button
                    Box(
                        modifier = Modifier
                            .size(34.dp)
                            .background(Color(0x22FFFFFF), RoundedCornerShape(8.dp))
                            .clickable { onOpenSettings() },
                        contentAlignment = Alignment.Center
                    ) {
                        Text(text = "⚙️", fontSize = 16.sp)
                    }

                    // Debug Button
                    Box(
                        modifier = Modifier
                            .size(34.dp)
                            .background(Color(0x22FFFFFF), RoundedCornerShape(8.dp))
                            .clickable { onToggleDebug() },
                        contentAlignment = Alignment.Center
                    ) {
                        Text(text = "🐞", fontSize = 16.sp)
                    }
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
// Debug Overlay (Section 41)
// ==========================================

@Composable
fun DebugInfoOverlay(
    trackingState: String,
    camX: Float,
    camY: Float,
    camZ: Float,
    qx: Float,
    qy: Float,
    qz: Float,
    qw: Float,
    serverPose: APPosition,
    selectedAP: AccessPointUIState?,
    calibration: CalibrationState,
    connection: ConnectionStatus,
    apCount: Int,
    localizedCount: Int,
    scanCount: Int,
    modifier: Modifier = Modifier
) {
    // Calculate Yaw, Pitch, Roll from quaternion
    val sinr_cosp = 2f * (qw * qx + qy * qz)
    val cosr_cosp = 1f - 2f * (qx * qx + qy * qy)
    val roll = Math.toDegrees(atan2(sinr_cosp.toDouble(), cosr_cosp.toDouble())).toFloat()

    val sinp = 2f * (qw * qy - qz * qx)
    val pitch = if (abs(sinp) >= 1) Math.toDegrees(Math.copySign(Math.PI / 2, sinp.toDouble())).toFloat()
                else Math.toDegrees(Math.asin(sinp.toDouble())).toFloat()

    val siny_cosp = 2f * (qw * qz + qx * qy)
    val cosy_cosp = 1f - 2f * (qy * qy + qz * qz)
    val yaw = Math.toDegrees(atan2(siny_cosp.toDouble(), cosy_cosp.toDouble())).toFloat()

    Card(
        shape = RoundedCornerShape(8.dp),
        colors = CardDefaults.cardColors(containerColor = Color(0xEE000000)),
        modifier = modifier.width(300.dp)
    ) {
        Column(modifier = Modifier.padding(10.dp)) {
            Text(
                text = "TELEMETRY DEBUG (SPATIAL)",
                style = MaterialTheme.typography.labelMedium,
                fontWeight = FontWeight.Bold,
                color = Color.Yellow
            )
            Spacer(modifier = Modifier.height(4.dp))
            val debugText = buildString {
                appendLine("TRACKING: $trackingState")
                appendLine("PHONE AR POS: (%.2f, %.2f, %.2f)".format(camX, camY, camZ))
                appendLine("PHONE ROTATION: Y:%.1f° P:%.1f° R:%.1f°".format(yaw, pitch, roll))
                appendLine("PHONE SERVER POS: (%.2f, %.2f, %.2f)".format(serverPose.x, serverPose.y, serverPose.z))
                appendLine("CALIBRATION: ${if (calibration.isCalibrated) "VALID" else "INVALID"}")
                appendLine("SERVER WS: ${connection.name}")
                appendLine("RAW SCANS: $scanCount | APs: $localizedCount/$apCount")
                appendLine("------------------------------")
                if (selectedAP != null) {
                    appendLine("SELECTED AP: ${selectedAP.ssid}")
                    val sPos = selectedAP.serverPosition
                    if (sPos != null) {
                        appendLine("AP SERVER POS: (%.2f, %.2f, %.2f)".format(sPos.x, sPos.y, sPos.z))
                    } else {
                        appendLine("AP SERVER POS: None")
                    }
                    if (selectedAP.arPositionX != null) {
                        appendLine("AP AR POS: (%.2f, %.2f, %.2f)".format(selectedAP.arPositionX, selectedAP.arPositionY, selectedAP.arPositionZ))
                    } else {
                        appendLine("AP AR POS: None (Not localized)")
                    }
                    appendLine("DISTANCE: ${selectedAP.distanceToUserM?.let { "%.2f m".format(it) } ?: "-- m"}")
                    appendLine("LOCAL RSSI: ${selectedAP.phoneRssiDbm?.let { "$it dBm" } ?: "Searching"}")
                    appendLine("CONFIDENCE: ${(selectedAP.confidence * 100).toInt()}%")
                    appendLine("ERROR: ±%.1f m".format(selectedAP.errorRadiusM))
                    appendLine("OBSERVATIONS: ${selectedAP.observationCount} | HUBS: ${selectedAP.hubCount}")
                } else {
                    appendLine("SELECTED AP: None (Tap to inspect)")
                }
            }
            Text(
                text = debugText,
                style = MaterialTheme.typography.bodySmall,
                fontFamily = FontFamily.Monospace,
                color = Color(0xFF00FF66),
                fontSize = 10.5.sp,
                lineHeight = 14.sp
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
                                } else {
                                    Text(
                                        text = "Insufficient localization data (%d obs, %d hubs)".format(ap.observationCount, ap.hubCount),
                                        style = MaterialTheme.typography.labelSmall,
                                        color = Color.Gray
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

// ==========================================
// Hub Location Marker & AR Billboard
// ==========================================

@Composable
fun HubLocationMarkerOverlay(
    screenPoint: ScreenPoint,
    hub: HubUIState,
    onSelect: () -> Unit
) {
    if (screenPoint.screenX.isNaN() || screenPoint.screenY.isNaN() || screenPoint.screenX < -1000f || screenPoint.screenY < -1000f) return
    val density = LocalDensity.current
    val sx = screenPoint.screenX.roundToInt()
    val sy = screenPoint.screenY.roundToInt()

    Box(
        modifier = Modifier
            .offset { IntOffset(sx, sy) }
    ) {
        // --- Center Hub Pin Point (Purple glowing beacon) ---
        Box(
            modifier = Modifier
                .size(16.dp)
                .offset((-8).dp, (-8).dp)
                .background(Color.White, CircleShape)
                .border(3.dp, if (hub.isSelected) Color.Yellow else Color(0xFF9C27B0), CircleShape)
        )

        // --- Floating Billboard HUD Card ---
        Card(
            shape = RoundedCornerShape(10.dp),
            colors = CardDefaults.cardColors(
                containerColor = if (hub.isSelected) Color(0xDD3A1C5A) else Color(0xDD1E1035)
            ),
            border = if (hub.isSelected) borderCardStroke(Color.Yellow) else borderCardStroke(Color(0x999C27B0)),
            modifier = Modifier
                .offset(x = (-80).dp, y = (-110).dp)
                .width(170.dp)
                .clickable { onSelect() }
        ) {
            Column(modifier = Modifier.padding(8.dp)) {
                // Hub Name / ID
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    modifier = Modifier.fillMaxWidth()
                ) {
                    Text(
                        text = "💻",
                        fontSize = 14.sp
                    )
                    Spacer(modifier = Modifier.width(4.dp))
                    Text(
                        text = hub.hubId,
                        style = MaterialTheme.typography.titleSmall,
                        fontWeight = FontWeight.Bold,
                        color = Color.White,
                        maxLines = 1
                    )
                }

                Spacer(modifier = Modifier.height(4.dp))

                // Distance
                Text(
                    text = hub.distanceToUserM?.let { "%.1fm away".format(it) } ?: "Anchored",
                    style = MaterialTheme.typography.bodyMedium,
                    fontWeight = FontWeight.Bold,
                    color = Color(0xFFCE93D8)
                )

                // Status & Observation Count
                Text(
                    text = if (hub.isCalibrated) "Calibrated • %d obs".format(hub.observationCount) else "Uncalibrated",
                    style = MaterialTheme.typography.labelSmall,
                    color = if (hub.isCalibrated) Color(0xFF81C784) else Color(0xFFFFB74D)
                )
            }
        }
    }
}

// ==========================================
// Hub Off-Screen Directional Indicator
// ==========================================

@Composable
fun HubOffScreenDirectionIndicator(
    screenPoint: ScreenPoint,
    hub: HubUIState,
    onClick: () -> Unit
) {
    if (screenPoint.screenX.isNaN() || screenPoint.screenY.isNaN() || screenPoint.screenX < -1000f || screenPoint.screenY < -1000f) return
    val x = screenPoint.screenX.roundToInt()
    val y = screenPoint.screenY.roundToInt()
    val angle = if (screenPoint.edgeAngleDegrees.isNaN() || screenPoint.edgeAngleDegrees.isInfinite()) 0f else screenPoint.edgeAngleDegrees

    Box(
        modifier = Modifier
            .offset { IntOffset(x - 45, y - 25) }
            .background(Color(0xDD9C27B0), RoundedCornerShape(16.dp))
            .clickable { onClick() }
            .padding(horizontal = 10.dp, vertical = 6.dp)
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = "➤",
                fontSize = 13.sp,
                fontWeight = FontWeight.Bold,
                color = Color.White,
                modifier = Modifier.rotate(angle)
            )
            Spacer(modifier = Modifier.width(4.dp))
            Text(
                text = "${hub.hubId} (${hub.distanceToUserM?.let { "%.1fm".format(it) } ?: "..."})",
                style = MaterialTheme.typography.labelMedium,
                fontWeight = FontWeight.Bold,
                color = Color.White
            )
        }
    }
}

// ==========================================
// Venue Hubs Sheet (Auto-Discovery & 1-Tap Anchor)
// ==========================================

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun VenueHubsBottomSheet(
    hubs: List<HubUIState>,
    cameraX: Float,
    cameraY: Float,
    cameraZ: Float,
    transformer: CoordinateTransformer,
    onAnchorHub: (hubId: String, sX: Double, sY: Double, sZ: Double) -> Unit,
    onAlignArToHub: (HubUIState) -> Unit,
    onSelectHub: (String) -> Unit,
    onDismiss: () -> Unit
) {
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(16.dp)
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically
            ) {
                Text(
                    text = "Venue Wi-Fi Hubs (${hubs.size})",
                    style = MaterialTheme.typography.titleMedium,
                    fontWeight = FontWeight.Bold
                )
                Text(
                    text = "${hubs.count { it.isCalibrated }}/${hubs.size} Calibrated",
                    style = MaterialTheme.typography.labelSmall,
                    color = if (hubs.all { it.isCalibrated } && hubs.isNotEmpty()) Color(0xFF388E3C) else Color(0xFFF57C00),
                    fontWeight = FontWeight.SemiBold
                )
            }

            Spacer(modifier = Modifier.height(4.dp))

            Text(
                text = "Automatic hub discovery enabled. Walk over to any laptop running a hub agent and tap 'Anchor Here' to lock its exact physical 3D location.",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant
            )

            Spacer(modifier = Modifier.height(12.dp))

            if (hubs.isEmpty()) {
                Box(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(32.dp),
                    contentAlignment = Alignment.Center
                ) {
                    Column(horizontalAlignment = Alignment.CenterHorizontally) {
                        Text(text = "💻", fontSize = 36.sp)
                        Spacer(modifier = Modifier.height(8.dp))
                        Text(
                            text = "No active hubs detected",
                            style = MaterialTheme.typography.titleSmall,
                            fontWeight = FontWeight.Bold
                        )
                        Spacer(modifier = Modifier.height(4.dp))
                        Text(
                            text = "Start python main.py on your venue laptops. They will appear here automatically via WebSocket.",
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            textAlign = androidx.compose.ui.text.style.TextAlign.Center
                        )
                    }
                }
            } else {
                LazyColumn(modifier = Modifier.fillMaxHeight(0.6f)) {
                    items(hubs) { hub ->
                        HubItemCard(
                            hub = hub,
                            onAnchorHere = {
                                val sPose = transformer.arToServer(cameraX, cameraY, cameraZ)
                                onAnchorHub(hub.hubId, sPose.x, sPose.y, sPose.z)
                            },
                            onAlignArToHub = { onAlignArToHub(hub) },
                            onSelect = { onSelectHub(hub.hubId) }
                        )
                    }
                }
            }
        }
    }
}

@Composable
fun HubItemCard(
    hub: HubUIState,
    onAnchorHere: () -> Unit,
    onAlignArToHub: () -> Unit,
    onSelect: () -> Unit
) {
    Card(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 6.dp),
        colors = CardDefaults.cardColors(
            containerColor = if (hub.isSelected) MaterialTheme.colorScheme.primaryContainer else MaterialTheme.colorScheme.surfaceVariant
        )
    ) {
        Column(modifier = Modifier.padding(14.dp)) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically
            ) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(text = "💻", fontSize = 18.sp)
                    Spacer(modifier = Modifier.width(8.dp))
                    Column {
                        Text(
                            text = hub.hubId,
                            style = MaterialTheme.typography.titleSmall,
                            fontWeight = FontWeight.Bold
                        )
                        Text(
                            text = "${hub.platform.ifEmpty { "Venue Laptop" }} • ${hub.observationCount} scans received",
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant
                        )
                    }
                }

                Surface(
                    color = if (hub.isCalibrated) Color(0x334CAF50) else Color(0x33FF9800),
                    shape = RoundedCornerShape(12.dp)
                ) {
                    Text(
                        text = if (hub.isCalibrated) "Calibrated" else "Needs Calibration",
                        color = if (hub.isCalibrated) Color(0xFF2E7D32) else Color(0xFFE65100),
                        style = MaterialTheme.typography.labelSmall,
                        fontWeight = FontWeight.Bold,
                        modifier = Modifier.padding(horizontal = 8.dp, vertical = 4.dp)
                    )
                }
            }

            Spacer(modifier = Modifier.height(10.dp))

            if (hub.isCalibrated) {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    Text(
                        text = "Pos: (%.1f, %.1f, %.1f) m".format(hub.serverPositionX, hub.serverPositionY, hub.serverPositionZ),
                        style = MaterialTheme.typography.bodySmall,
                        fontFamily = FontFamily.Monospace,
                        color = MaterialTheme.colorScheme.onSurfaceVariant
                    )
                    if (hub.distanceToUserM != null) {
                        Text(
                            text = "~%.1f m away".format(hub.distanceToUserM),
                            style = MaterialTheme.typography.bodySmall,
                            fontWeight = FontWeight.Bold,
                            color = Color(0xFF7C4DFF)
                        )
                    }
                }
            } else {
                Text(
                    text = "Default / unverified location. Bring phone near laptop to anchor accurately.",
                    style = MaterialTheme.typography.labelSmall,
                    color = Color(0xFFE65100)
                )
            }

            Spacer(modifier = Modifier.height(10.dp))

            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.End
            ) {
                if (hub.isCalibrated) {
                    OutlinedButton(
                        onClick = onSelect,
                        modifier = Modifier.padding(end = 6.dp)
                    ) {
                        Text("Highlight")
                    }
                    OutlinedButton(
                        onClick = onAlignArToHub,
                        modifier = Modifier.padding(end = 6.dp),
                        colors = ButtonDefaults.outlinedButtonColors(contentColor = Color(0xFF2E7D32))
                    ) {
                        Text("📍 Align AR")
                    }
                    Button(
                        onClick = onAnchorHere,
                        colors = ButtonDefaults.buttonColors(containerColor = Color(0xFF7C4DFF))
                    ) {
                        Text("Re-Anchor")
                    }
                } else {
                    Button(
                        onClick = onAnchorHere,
                        colors = ButtonDefaults.buttonColors(containerColor = Color(0xFF7C4DFF))
                    ) {
                        Text("📍 Anchor Here (Phone Position)")
                    }
                }
            }
        }
    }
}