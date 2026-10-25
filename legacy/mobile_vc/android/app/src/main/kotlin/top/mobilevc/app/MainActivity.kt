package top.mobilevc.app

import android.content.Context
import android.content.Intent
import android.os.Bundle
import android.os.PowerManager
import androidx.annotation.NonNull
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel

class MainActivity : FlutterActivity() {
    private val channelName = "top.mobilevc.app/background_keep_alive"
    private val deepLinkChannelName = "top.mobilevc.app/deep_link"
    private var wakeLock: PowerManager.WakeLock? = null
    private var deepLinkChannel: MethodChannel? = null
    private var pendingDeepLink: String? = null

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
        deepLinkChannel = MethodChannel(
            flutterEngine.dartExecutor.binaryMessenger,
            deepLinkChannelName,
        ).also { channel ->
            channel.setMethodCallHandler { call, result ->
                when (call.method) {
                    "takeInitialLink" -> result.success(takePendingOrIntentLink())
                    else -> result.notImplemented()
                }
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        val link = relayLinkFromIntent(intent) ?: return
        clearCurrentRelayIntent()
        val channel = deepLinkChannel
        if (channel == null) {
            pendingDeepLink = link
            return
        }
        channel.invokeMethod("onLink", link)
    }

    override fun onDestroy() {
        stopKeepAlive()
        deepLinkChannel = null
        super.onDestroy()
    }

    private fun takePendingOrIntentLink(): String? {
        pendingDeepLink?.let { link ->
            pendingDeepLink = null
            clearCurrentRelayIntent()
            return link
        }
        val link = relayLinkFromIntent(intent) ?: return null
        clearCurrentRelayIntent()
        return link
    }

    private fun clearCurrentRelayIntent() {
        val current = intent ?: return
        if (relayLinkFromIntent(current) == null) {
            return
        }
        setIntent(Intent(current).apply { data = null })
    }

    private fun relayLinkFromIntent(intent: Intent?): String? {
        if (intent?.action != Intent.ACTION_VIEW) {
            return null
        }
        val data = intent.data ?: return null
        if (data.scheme != "mobilevc" || data.host != "relay") {
            return null
        }
        return data.toString()
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
