package com.example.wifiapp.network

import android.util.Log
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okio.ByteString

class WebSocketManager {
    private val client = OkHttpClient()
    private var webSocket: WebSocket? = null

    private val _connectionState = MutableStateFlow(false)
    val connectionState: StateFlow<Boolean> = _connectionState.asStateFlow()

    private val _incomingMessages = MutableStateFlow<String?>(null)
    val incomingMessages: StateFlow<String?> = _incomingMessages.asStateFlow()

    fun connect(url: String) {
        val request = Request.Builder().url(url).build()

        webSocket = client.newWebSocket(request, object : WebSocketListener() {
            override fun onOpen(webSocket: WebSocket, response: Response) {
                _connectionState.value = true
                Log.d("WebSocketManager", "Connected to server!")
            }

            override fun onMessage(webSocket: WebSocket, text: String) {
                _incomingMessages.value = text
                Log.d("WebSocketManager", "Message received: $text")
            }

            override fun onMessage(webSocket: WebSocket, bytes: ByteString) {
                Log.d("WebSocketManager", "Binary message received")
            }

            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
                _connectionState.value = false
                Log.d("WebSocketManager", "Disconnected: $reason")
            }

            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                _connectionState.value = false
                Log.e("WebSocketManager", "Error: ${t.message}")
            }
        })
    }

    fun sendMessage(message: String) {
        webSocket?.send(message)
    }

    fun disconnect() {
        webSocket?.close(1000, "User disconnected")
        webSocket = null
    }
}