package org.plebian.app

import android.content.Context
import android.content.Intent
import io.flutter.embedding.android.FlutterFragmentActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.embedding.engine.FlutterEngineCache
import io.flutter.embedding.engine.FlutterShellArgs
import io.flutter.embedding.engine.dart.DartExecutor
import io.flutter.plugin.common.MethodChannel

/**
 * One Flutter engine for the process, cached so it outlives the activity:
 * LinkService keeps the process (and so the Omi and server links running in
 * this engine) alive with the app closed. The activity only attaches to it.
 */
object Engine {
    const val ID = "main"

    fun ensure(context: Context, args: Array<String> = emptyArray()): FlutterEngine {
        FlutterEngineCache.getInstance().get(ID)?.let { return it }
        val app = context.applicationContext
        val engine = FlutterEngine(app, args)
        MethodChannel(engine.dartExecutor.binaryMessenger, "org.plebian.app/service").setMethodCallHandler { call, result ->
            if (call.method == "start") {
                app.startForegroundService(Intent(app, LinkService::class.java))
                result.success(null)
            } else {
                result.notImplemented()
            }
        }
        engine.dartExecutor.executeDartEntrypoint(DartExecutor.DartEntrypoint.createDefault())
        FlutterEngineCache.getInstance().put(ID, engine)
        return engine
    }
}

// FlutterFragmentActivity: biometric_signature shows BiometricPrompt from a FragmentActivity.
class MainActivity : FlutterFragmentActivity() {
    override fun getCachedEngineId(): String {
        Engine.ensure(this, FlutterShellArgs.fromIntent(intent).toArray())
        return Engine.ID
    }

    override fun shouldDestroyEngineWithHost(): Boolean = false
}
