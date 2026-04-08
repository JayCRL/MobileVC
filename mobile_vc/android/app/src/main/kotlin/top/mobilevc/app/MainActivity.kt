package top.mobilevc.app

import android.content.Context
import android.os.Bundle
import android.os.PowerManager
import androidx.annotation.NonNull
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel

class MainActivity : FlutterActivity() {
    private val channelName = "top.mobilevc.app/background_keep_alive"
    private var wakeLock: PowerManager.WakeLock? = null

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
