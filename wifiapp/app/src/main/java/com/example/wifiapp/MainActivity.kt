package com.example.wifiapp

import android.Manifest
import android.content.pm.PackageManager
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import com.example.wifiapp.network.WebSocketManager
import io.github.sceneview.ar.ARScene
import com.google.ar.core.TrackingState

// Updated Node Imports
import io.github.sceneview.node.Node
import io.github.sceneview.node.ModelNode
import io.github.sceneview.ar.node.AnchorNode

import io.github.sceneview.math.Position
import io.github.sceneview.rememberEngine
import io.github.sceneview.rememberModelLoader
import kotlinx.coroutines.launch

// ML Kit Imports
import com.google.mlkit.vision.barcode.BarcodeScannerOptions
import com.google.mlkit.vision.barcode.BarcodeScanning
import com.google.mlkit.vision.barcode.common.Barcode
import com.google.mlkit.vision.common.InputImage

// Setup ML Kit to scan QR codes
val options = BarcodeScannerOptions.Builder()
    .setBarcodeFormats(Barcode.FORMAT_QR_CODE)
    .build()
val scanner = BarcodeScanning.getClient(options)

class MainActivity : ComponentActivity() {
    private val webSocketManager = WebSocketManager()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent {
            MaterialTheme {
                Surface(
                    modifier = Modifier.fillMaxSize(),
                    color = MaterialTheme.colorScheme.background
                ) {
                    AppUI(webSocketManager)
                }
            }
        }
    }
}

@Composable
fun AppUI(webSocketManager: WebSocketManager) {
    val context = LocalContext.current
    var hasCameraPermission by remember {
        mutableStateOf(
            ContextCompat.checkSelfPermission(
                context, Manifest.permission.CAMERA
            ) == PackageManager.PERMISSION_GRANTED
        )
    }

    val permissionLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) { isGranted -> hasCameraPermission = isGranted }

    if (!hasCameraPermission) {
        Column(
            modifier = Modifier.fillMaxSize(),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.Center
        ) {
            Text("We need the camera to track AprilTags for ARCore.")
            Spacer(modifier = Modifier.height(16.dp))
            Button(onClick = { permissionLauncher.launch(Manifest.permission.CAMERA) }) {
                Text("Grant Camera Permission")
            }
        }
    } else {
        DashboardScreen(webSocketManager)
    }
}

@Composable
fun DashboardScreen(webSocketManager: WebSocketManager) {
    val isConnected by webSocketManager.connectionState.collectAsState()
    var serverUrl by remember { mutableStateOf("wss://nah.sreedevss.in/ws") }

    var cameraPoseText by remember { mutableStateOf("Initializing spatial tracking...") }
    var lastSendTime by remember { mutableLongStateOf(0L) }

    // This prevents the ML Kit scanner from freezing by processing one frame at a time
    var isProcessingImage by remember { mutableStateOf(false) }

    // Prevents spawning multiple models while the first one is loading in the background
    var isModelPlaced by remember { mutableStateOf(false) }

    // Updated: Changed from ModelNode to Node so it can hold our AnchorNode
    val childNodes = remember { mutableStateListOf<Node>() }

    // Hooks for loading 3D models in the background
    val engine = rememberEngine()
    val modelLoader = rememberModelLoader(engine)
    val coroutineScope = rememberCoroutineScope()

    Column(
        modifier = Modifier.fillMaxSize().padding(16.dp),
        horizontalAlignment = Alignment.CenterHorizontally
    ) {
        Text(text = "Spatial Hub Dashboard", style = MaterialTheme.typography.headlineSmall)
        Spacer(modifier = Modifier.height(16.dp))

        OutlinedTextField(
            value = serverUrl,
            onValueChange = { serverUrl = it },
            label = { Text("Server URL") },
            modifier = Modifier.fillMaxWidth()
        )
        Spacer(modifier = Modifier.height(8.dp))

        Button(
            onClick = { if (isConnected) webSocketManager.disconnect() else webSocketManager.connect(serverUrl) },
            modifier = Modifier.fillMaxWidth()
        ) {
            Text(if (isConnected) "Disconnect" else "Connect to Server")
        }
        Spacer(modifier = Modifier.height(8.dp))

        Text(
            text = if (isConnected) "Status: CONNECTED" else "Status: DISCONNECTED",
            color = if (isConnected) Color.Green else Color.Red
        )
        Spacer(modifier = Modifier.height(16.dp))

        Box(
            modifier = Modifier.fillMaxWidth().weight(1f)
        ) {
            ARScene(
                modifier = Modifier.fillMaxSize(),
                engine = engine,
                modelLoader = modelLoader,
                childNodes = childNodes,
                onSessionUpdated = { session, frame ->
                    val camera = frame.camera
                    if (camera.trackingState == TrackingState.TRACKING) {
                        val pose = camera.pose
                        val x = pose.tx()
                        val y = pose.ty()
                        val z = pose.tz()

                        // Update HUD unless a QR code is currently locked on
                        if (!cameraPoseText.startsWith("ANCHOR FOUND")) {
                            cameraPoseText = String.format("X: %+.3f\nY: %+.3f\nZ: %+.3f", x, y, z)
                        }

                        // Send tracking data to the server at 10Hz
                        if (isConnected) {
                            val currentTime = System.currentTimeMillis()
                            if (currentTime - lastSendTime > 100) {
                                val jsonPayload = """{"type": "pose", "x": $x, "y": $y, "z": $z}"""
                                webSocketManager.sendMessage(jsonPayload)
                                lastSendTime = currentTime
                            }
                        }

                        // Extract the frame and scan for QR Codes
                        if (!isProcessingImage) {
                            try {
                                val image = frame.acquireCameraImage()
                                if (image != null) {
                                    isProcessingImage = true
                                    val inputImage = InputImage.fromMediaImage(image, 0)

                                    scanner.process(inputImage)
                                        .addOnSuccessListener { barcodes ->
                                            for (barcode in barcodes) {
                                                val qrValue = barcode.rawValue
                                                val boundingBox = barcode.boundingBox // Get the 2D box on screen

                                                if (qrValue != null && boundingBox != null) {
                                                    cameraPoseText = "ANCHOR FOUND: $qrValue\nLocal X: $x, Y: $y, Z: $z"

                                                    // Send anchor data to server
                                                    if (isConnected) {
                                                        val anchorPayload = """{"type": "anchor", "id": "$qrValue", "x": $x, "y": $y, "z": $z}"""
                                                        webSocketManager.sendMessage(anchorPayload)
                                                    }

                                                    // RAYCASTING: Shoot a line from the center of the QR code on screen into the 3D world
                                                    val hitTestResults = frame.hitTest(
                                                        boundingBox.exactCenterX(),
                                                        boundingBox.exactCenterY()
                                                    )

                                                    val firstHit = hitTestResults.firstOrNull()

                                                    // Ensure we only place the model once
                                                    if (firstHit != null && childNodes.isEmpty() && !isModelPlaced) {

                                                        isModelPlaced = true

                                                        coroutineScope.launch {
                                                            // Ensure you placed "box.glb" in the app/src/main/assets/ folder
                                                            val modelInstance = modelLoader.loadModelInstance("box.glb")

                                                            if (modelInstance != null) {
                                                                // 1. Create a physical ARCore Anchor at the hit location
                                                                val anchor = firstHit.createAnchor()

                                                                // 2. Create an AnchorNode to track that physical point
                                                                val anchorNode = AnchorNode(engine, anchor)

                                                                // 3. Create the 3D Model Node
                                                                val modelNode = ModelNode(modelInstance = modelInstance)

                                                                // 4. Attach the model to the anchor, and the anchor to the scene
                                                                anchorNode.addChildNode(modelNode)
                                                                childNodes.add(anchorNode)
                                                            } else {
                                                                isModelPlaced = false
                                                            }
                                                        }
                                                    }
                                                }
                                            }
                                        }
                                        .addOnCompleteListener {
                                            image.close() // CRITICAL: Free up the memory
                                            android.os.Handler(android.os.Looper.getMainLooper()).postDelayed({
                                                isProcessingImage = false
                                            }, 500)
                                        }
                                }
                            } catch (e: Exception) {
                                isProcessingImage = false
                            }
                        }
                    } else {
                        cameraPoseText = "Searching for surfaces..."
                    }
                }
            )

            Box(
                modifier = Modifier
                    .padding(16.dp)
                    .align(Alignment.TopStart)
                    .background(Color.Black.copy(alpha = 0.7f), RoundedCornerShape(8.dp))
                    .padding(12.dp)
            ) {
                Text(
                    text = cameraPoseText,
                    color = Color.Green,
                    fontFamily = FontFamily.Monospace
                )
            }
        }
    }
}