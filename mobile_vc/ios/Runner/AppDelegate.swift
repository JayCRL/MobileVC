import Flutter
import UIKit

@main
@objc class AppDelegate: FlutterAppDelegate {
  private let keepAliveChannel = "top.mobilevc.app/background_keep_alive"
  private var backgroundTask: UIBackgroundTaskIdentifier = .invalid

  override func application(
    _ application: UIApplication,
    didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]?
  ) -> Bool {
    GeneratedPluginRegistrant.register(with: self)
    if let controller = window?.rootViewController as? FlutterViewController {
      let channel = FlutterMethodChannel(
        name: keepAliveChannel,
        binaryMessenger: controller.binaryMessenger
      )
      channel.setMethodCallHandler { [weak self] call, result in
        switch call.method {
        case "start":
          let timeoutMs = (call.arguments as? [String: Any])?["timeoutMs"] as? NSNumber
          self?.beginBackgroundKeepAlive(timeoutMs: timeoutMs?.doubleValue ?? 90_000)
          result(nil)
        case "stop":
          self?.endBackgroundKeepAlive()
          result(nil)
        default:
          result(FlutterMethodNotImplemented)
        }
      }
    }
    return super.application(application, didFinishLaunchingWithOptions: launchOptions)
  }

  override func applicationWillTerminate(_ application: UIApplication) {
    endBackgroundKeepAlive()
    super.applicationWillTerminate(application)
  }

  private func beginBackgroundKeepAlive(timeoutMs: Double) {
    endBackgroundKeepAlive()
    backgroundTask = UIApplication.shared.beginBackgroundTask(withName: "mobilevc.reply_keep_alive") {
      self.endBackgroundKeepAlive()
    }
    if backgroundTask == .invalid {
      return
    }
    let clampedDelay = max(1, timeoutMs / 1000.0)
    DispatchQueue.main.asyncAfter(deadline: .now() + clampedDelay) { [weak self] in
      self?.endBackgroundKeepAlive()
    }
  }

  private func endBackgroundKeepAlive() {
    guard backgroundTask != .invalid else {
      return
    }
    UIApplication.shared.endBackgroundTask(backgroundTask)
    backgroundTask = .invalid
  }
}
