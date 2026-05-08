import Flutter
import UIKit
import UserNotifications

@main
@objc class AppDelegate: FlutterAppDelegate {
  private let keepAliveChannel = "top.mobilevc.app/background_keep_alive"
  private let pushChannelName = "top.mobilevc.app/push"
  private var backgroundTask: UIBackgroundTaskIdentifier = .invalid
  private var pushChannel: FlutterMethodChannel?
  private var deviceTokenHex: String = ""
  private var pendingPushOpenPayload: [String: Any]?

  override func application(
    _ application: UIApplication,
    didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]?
  ) -> Bool {
    GeneratedPluginRegistrant.register(with: self)
    let launched = super.application(application, didFinishLaunchingWithOptions: launchOptions)
    configureMethodChannels(application: application)
    UNUserNotificationCenter.current().delegate = self
    return launched
  }

  override func applicationWillTerminate(_ application: UIApplication) {
    endBackgroundKeepAlive()
    super.applicationWillTerminate(application)
  }

  override func application(
    _ application: UIApplication,
    didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data
  ) {
    let token = deviceToken.map { String(format: "%02x", $0) }.joined()
    deviceTokenHex = token
    pushChannel?.invokeMethod("onToken", arguments: token)
    super.application(application, didRegisterForRemoteNotificationsWithDeviceToken: deviceToken)
  }

  override func application(
    _ application: UIApplication,
    didFailToRegisterForRemoteNotificationsWithError error: Error
  ) {
    let message = "APNs registration failed: \(error.localizedDescription)"
    NSLog("[push] \(message)")
    pushChannel?.invokeMethod("onRegistrationError", arguments: message)
    super.application(application, didFailToRegisterForRemoteNotificationsWithError: error)
  }

  override func userNotificationCenter(
    _ center: UNUserNotificationCenter,
    willPresent notification: UNNotification,
    withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void
  ) {
    pushChannel?.invokeMethod("onMessageReceived", arguments: notificationPayload(userInfo: notification.request.content.userInfo))
    completionHandler([.banner, .badge, .sound])
  }

  override func userNotificationCenter(
    _ center: UNUserNotificationCenter,
    didReceive response: UNNotificationResponse,
    withCompletionHandler completionHandler: @escaping () -> Void
  ) {
    let payload = notificationPayload(userInfo: response.notification.request.content.userInfo)
    pendingPushOpenPayload = payload
    pushChannel?.invokeMethod("onMessageOpenedApp", arguments: payload)
    completionHandler()
  }

  private func configureMethodChannels(application: UIApplication) {
    guard let controller = window?.rootViewController as? FlutterViewController else {
      NSLog("[push] FlutterViewController unavailable while configuring channels")
      return
    }

    let keepAlive = FlutterMethodChannel(
      name: keepAliveChannel,
      binaryMessenger: controller.binaryMessenger
    )
    keepAlive.setMethodCallHandler { [weak self] call, result in
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

    let pushChannel = FlutterMethodChannel(
      name: pushChannelName,
      binaryMessenger: controller.binaryMessenger
    )
    pushChannel.setMethodCallHandler { [weak self] call, result in
      guard let self else {
        result(FlutterError(code: "unavailable", message: "app delegate unavailable", details: nil))
        return
      }
      switch call.method {
      case "requestPermissionAndRegister":
        self.requestPushPermission(application: application, result: result)
      case "getDeviceToken":
        result(self.deviceTokenHex.isEmpty ? nil : self.deviceTokenHex)
      default:
        result(FlutterMethodNotImplemented)
      }
    }
    self.pushChannel = pushChannel

    // iSH Linux bridge
    let ishChannel = FlutterMethodChannel(
      name: "mobilevc/ish",
      binaryMessenger: controller.binaryMessenger
    )
    ishChannel.setMethodCallHandler { call, result in
      switch call.method {
      case "init":
        NSLog("[iSH] init requested")
        let bundleAlpine = Bundle.main.bundlePath + "/alpine"
        let writableAlpine = NSTemporaryDirectory() + "alpine"
        let fm = FileManager.default
        var isDir: ObjCBool = false
        guard fm.fileExists(atPath: bundleAlpine, isDirectory: &isDir), isDir.boolValue else {
          NSLog("[iSH] ERROR: Alpine not found at \(bundleAlpine)")
          result(FlutterError(code: "no_rootfs", message: "Alpine rootfs not found at \(bundleAlpine)", details: nil))
          return
        }
        NSLog("[iSH] Alpine found, copying to \(writableAlpine)")
        if !fm.fileExists(atPath: writableAlpine + "/data") {
          try? fm.removeItem(atPath: writableAlpine)
          do {
            try fm.copyItem(atPath: bundleAlpine, toPath: writableAlpine)
            NSLog("[iSH] Alpine copied OK")
          } catch {
            NSLog("[iSH] ERROR copy: \(error)")
            result(FlutterError(code: "copy_failed", message: "\(error)", details: nil))
            return
          }
        }
        NSLog("[iSH] Calling ish_init on bg thread...")
        DispatchQueue.global(qos: .userInitiated).async {
          let status = ish_init(writableAlpine)
          NSLog("[iSH] ish_init returned \(status)")
          DispatchQueue.main.async {
            result(status == 0)
          }
        }
      case "exec":
        guard let command = call.arguments as? String else {
          result(FlutterError(code: "bad_args", message: "command required", details: nil))
          return
        }
        NSLog("[iSH] exec cmd: \(command.prefix(200))")
        DispatchQueue.global(qos: .userInitiated).async {
          var output: UnsafeMutablePointer<CChar>?
          var outputLen: Int = 0
          let exitCode = ish_exec(command, &output, &outputLen)
          NSLog("[iSH] exec done code=\(exitCode) len=\(outputLen)")
          let outputStr = output.map { String(cString: $0) } ?? ""
          if let ptr = output { free(ptr) }
          DispatchQueue.main.async {
            result(["exitCode": exitCode, "output": outputStr])
          }
        }
      default:
        result(FlutterMethodNotImplemented)
      }
    }
  }

  private func requestPushPermission(application: UIApplication, result: @escaping FlutterResult) {
    UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .badge, .sound]) { granted, error in
      if let error {
        DispatchQueue.main.async {
          result(FlutterError(code: "push_permission_failed", message: error.localizedDescription, details: nil))
        }
        return
      }
      guard granted else {
        DispatchQueue.main.async {
          result(nil)
        }
        return
      }
      DispatchQueue.main.async {
        application.registerForRemoteNotifications()
        result(nil)
        if let payload = self.pendingPushOpenPayload {
          self.pushChannel?.invokeMethod("onMessageOpenedApp", arguments: payload)
          self.pendingPushOpenPayload = nil
        }
      }
    }
  }

  private func notificationPayload(userInfo: [AnyHashable: Any]) -> [String: Any] {
    let aps = userInfo["aps"] as? [String: Any]
    let alert = aps?["alert"] as? [String: Any]
    return [
      "title": alert?["title"] as? String ?? "",
      "body": alert?["body"] as? String ?? "",
      "data": userInfo.reduce(into: [String: Any]()) { partialResult, item in
        if let key = item.key as? String {
          partialResult[key] = item.value
        }
      },
    ]
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
