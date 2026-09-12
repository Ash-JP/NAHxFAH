package com.example.wifiapp.wifi

import android.Manifest
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.PackageManager
import android.net.wifi.ScanResult
import android.net.wifi.WifiManager
import android.os.Build
import android.util.Log
import androidx.core.content.ContextCompat
import com.example.wifiapp.models.LocalWifiScan
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch

class WifiScanner(private val context: Context) {
    companion object {
        private const val TAG = "WifiScanner"
        private const val SCAN_INTERVAL_MS = 3000L // 3s polling for fast spatial responsiveness
    }

    private val wifiManager = context.applicationContext.getSystemService(Context.WIFI_SERVICE) as? WifiManager

    private val _scanResults = MutableStateFlow<List<LocalWifiScan>>(emptyList())
    val scanResults: StateFlow<List<LocalWifiScan>> = _scanResults.asStateFlow()

    private val _lastScanTimestamp = MutableStateFlow(0L)
    val lastScanTimestamp: StateFlow<Long> = _lastScanTimestamp.asStateFlow()

    private val _isScanning = MutableStateFlow(false)
    val isScanning: StateFlow<Boolean> = _isScanning.asStateFlow()

    private val _isAvailable = MutableStateFlow(wifiManager != null)
    val isAvailable: StateFlow<Boolean> = _isAvailable.asStateFlow()

    private var scanJob: Job? = null
    private val scope = CoroutineScope(Dispatchers.IO)
    private var isReceiverRegistered = false

    private val wifiScanReceiver = object : BroadcastReceiver() {
        override fun onReceive(c: Context?, intent: Intent?) {
            try {
                if (intent?.action == WifiManager.SCAN_RESULTS_AVAILABLE_ACTION) {
                    val success = intent.getBooleanExtra(WifiManager.EXTRA_RESULTS_UPDATED, false)
                    Log.d(TAG, "Scan results available broadcast received (success=$success)")
                    processScanResults()
                }
            } catch (e: Exception) {
                Log.e(TAG, "Error in wifiScanReceiver: ${e.message}")
            }
        }
    }

    fun start() {
        if (wifiManager == null) {
            Log.e(TAG, "WifiManager not available on this device")
            _isAvailable.value = false
            return
        }

        val hasLocationPerm = ContextCompat.checkSelfPermission(
            context,
            Manifest.permission.ACCESS_FINE_LOCATION
        ) == PackageManager.PERMISSION_GRANTED

        if (!hasLocationPerm) {
            Log.w(TAG, "Cannot start WifiScanner: ACCESS_FINE_LOCATION permission not granted yet")
            return
        }

        if (!isReceiverRegistered) {
            try {
                val intentFilter = IntentFilter(WifiManager.SCAN_RESULTS_AVAILABLE_ACTION)
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                    context.registerReceiver(wifiScanReceiver, intentFilter, Context.RECEIVER_EXPORTED)
                } else {
                    context.registerReceiver(wifiScanReceiver, intentFilter)
                }
                isReceiverRegistered = true
            } catch (e: Exception) {
                Log.e(TAG, "Error registering wifi scan receiver: ${e.message}")
            }
        }

        // Process any cached scan results immediately
        processScanResults()

        // Start periodic scan and poll loop
        scanJob?.cancel()
        scanJob = scope.launch {
            while (isActive) {
                processScanResults()
                triggerScan()
                delay(SCAN_INTERVAL_MS)
            }
        }
    }

    fun stop() {
        scanJob?.cancel()
        scanJob = null
        if (isReceiverRegistered) {
            try {
                context.unregisterReceiver(wifiScanReceiver)
            } catch (e: Exception) {
                Log.w(TAG, "Error unregistering receiver: ${e.message}")
            }
            isReceiverRegistered = false
        }
        _isScanning.value = false
    }

    fun triggerScan() {
        if (wifiManager == null) return
        try {
            _isScanning.value = true
            @Suppress("DEPRECATION")
            val started = wifiManager.startScan()
            if (!started) {
                // If throttled by OS, fall back to reading latest available cached results
                Log.d(TAG, "startScan throttled by OS, reading latest cached scan results")
                processScanResults()
            }
        } catch (e: SecurityException) {
            Log.e(TAG, "Missing location or Wi-Fi permissions for scanning: ${e.message}")
            _isAvailable.value = false
        } catch (e: Exception) {
            Log.e(TAG, "Error triggering Wi-Fi scan: ${e.message}")
        } finally {
            _isScanning.value = false
        }
    }

    private fun processScanResults() {
        val manager = wifiManager ?: return
        try {
            val results: List<ScanResult> = manager.scanResults ?: emptyList()
            val now = System.currentTimeMillis()
            val parsedList = results.mapNotNull { result ->
                val bssid = result.BSSID ?: return@mapNotNull null
                val ssid = if (result.SSID.isNullOrEmpty()) "<hidden>" else result.SSID
                val rssi = result.level // Raw dBm from hardware
                val freq = result.frequency
                val channel = frequencyToChannel(freq)
                LocalWifiScan(
                    bssid = bssid.uppercase(),
                    ssid = ssid,
                    rssiDbm = rssi,
                    frequencyMhz = freq,
                    channel = channel,
                    timestampMs = now
                )
            }.sortedByDescending { it.rssiDbm }

            _scanResults.value = parsedList
            _lastScanTimestamp.value = now
            Log.d(TAG, "Processed ${parsedList.size} raw Wi-Fi observations")
        } catch (e: SecurityException) {
            Log.e(TAG, "Permission denied while fetching scan results: ${e.message}")
        } catch (e: Exception) {
            Log.e(TAG, "Error parsing scan results: ${e.message}")
        }
    }

    private fun frequencyToChannel(freqMHz: Int): Int {
        return when {
            freqMHz in 2412..2484 -> if (freqMHz == 2484) 14 else (freqMHz - 2407) / 5
            freqMHz in 5170..5825 -> (freqMHz - 5000) / 5
            freqMHz in 5925..7125 -> (freqMHz - 5950) / 5
            else -> 0
        }
    }
}
