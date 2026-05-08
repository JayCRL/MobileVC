package top.mobilevc.app

import android.content.Context
import android.os.Bundle
import android.os.PowerManager
import androidx.annotation.NonNull
import android.content.Intent
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.EventChannel
import io.flutter.plugin.common.MethodChannel

class MainActivity : FlutterActivity() {
    private val channelName = "top.mobilevc.app/background_keep_alive"
    private var wakeLock: PowerManager.WakeLock? = null
    private var pendingUri: String? = null
    private var deeplinkChannel: MethodChannel? = null
    private var uriEventSink: EventChannel.EventSink? = null

    override fun configureFlutterEngine(@NonNull flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        MethodChannel(
            flutterEngine.dartExecutor.binaryMessenger,
            channelName,
        ).setMethodCallHandler { call, result ->
            when (call.method) {
                "start" -> {
                    startKeepAlive((call.argument<Number>("timeoutMs")?.toLong() ?: 90000L))
                    result.success(null)
                }

                "stop" -> {
                    stopKeepAlive()
                    result.success(null)
                }

                else -> result.notImplemented()
            }
        }

        // Deep link channel
        deeplinkChannel = MethodChannel(
            flutterEngine.dartExecutor.binaryMessenger,
            "mobilevc/deeplink"
        )
        deeplinkChannel?.setMethodCallHandler { call, result ->
            when (call.method) {
                "getInitialUri" -> {
                    result.success(pendingUri)
                    pendingUri = null
                }
                else -> result.notImplemented()
            }
        }

        // EventChannel for streaming URIs
        EventChannel(
            flutterEngine.dartExecutor.binaryMessenger,
            "mobilevc/deeplink_uri"
        ).setStreamHandler(object : EventChannel.StreamHandler {
            override fun onListen(args: Any?, sink: EventChannel.EventSink) {
                uriEventSink = sink
                pendingUri?.let {
                    sink.success(it)
                    pendingUri = null
                }
            }
            override fun onCancel(args: Any?) {
                uriEventSink = null
            }
        })

        // Process intent that launched the activity
        handleIntent(intent)
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        handleIntent(intent)
    }

    private fun handleIntent(intent: Intent?) {
        val uri = intent?.data?.toString() ?: return
        if (!uri.startsWith("mobilevc://")) return
        deeplinkChannel?.invokeMethod("onUri", uri)
        uriEventSink?.success(uri)
        // If Flutter isn't ready yet, stash for getInitialUri
        if (deeplinkChannel == null) {
            pendingUri = uri
        }
    }

    override fun onDestroy() {
        stopKeepAlive()
        super.onDestroy()
    }

    private fun startKeepAlive(timeoutMs: Long) {
        val manager = getSystemService(Context.POWER_SERVICE) as? PowerManager ?: return
        val current = wakeLock
        if (current == null) {
            wakeLock = manager.newWakeLock(
                PowerManager.PARTIAL_WAKE_LOCK,
                "mobilevc:reply_keep_alive",
            ).apply {
                setReferenceCounted(false)
                acquire(timeoutMs.coerceAtLeast(1000L))
            }
            return
        }
        if (!current.isHeld) {
            current.acquire(timeoutMs.coerceAtLeast(1000L))
        }
    }

    private fun stopKeepAlive() {
        wakeLock?.let { lock ->
            if (lock.isHeld) {
                lock.release()
            }
        }
        wakeLock = null
    }
}
