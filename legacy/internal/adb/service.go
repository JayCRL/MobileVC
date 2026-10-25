package adb

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultCommandTimeout = 10 * time.Second
	frameCommandTimeout   = 15 * time.Second
)

type Device struct {
	Serial      string
	State       string
	Model       string
	Product     string
	DeviceName  string
	TransportID string
}

type Frame struct {
	Serial     string
	Format     string
	Data       []byte
	Width      int
	Height     int
	CapturedAt time.Time
}

type Status struct {
	ADBAvailable      bool
	ADBPath           string
	EmulatorAvailable bool
	EmulatorPath      string
	Devices           []Device
	AvailableAVDs     []string
	PreferredSerial   string
	PreferredAVD      string
	SuggestedAction   string
	Message           string
}

func ListDevices(ctx context.Context) ([]Device, error) {
	adbPath, err := resolveADBPath()
	if err != nil {
		return nil, err
	}
	return listDevicesWithPath(ctx, adbPath)
}

func listDevicesWithPath(ctx context.Context, adbPath string) ([]Device, error) {
	commandCtx, cancel := context.WithTimeout(ctx, defaultCommandTimeout)
	defer cancel()

	output, err := exec.CommandContext(commandCtx, adbPath, "devices", "-l").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("adb devices failed: %w: %s", err, strings.TrimSpace(string(output)))
	}

	lines := strings.Split(string(output), "\n")
	devices := make([]Device, 0, len(lines))
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "List of devices attached") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		item := Device{
			Serial: fields[0],
			State:  fields[1],
		}
		for _, field := range fields[2:] {
			key, value, ok := strings.Cut(field, ":")
			if !ok {
				continue
			}
			switch key {
			case "model":
				item.Model = value
			case "product":
				item.Product = value
			case "device":
				item.DeviceName = value
			case "transport_id":
				item.TransportID = value
			}
		}
		devices = append(devices, item)
	}
	return devices, nil
}

func ResolveSerial(ctx context.Context, requested string) (string, error) {
	serial := strings.TrimSpace(requested)
	if serial != "" {
		return serial, nil
	}

	devices, err := ListDevices(ctx)
	if err != nil {
		return "", err
	}
	connected := make([]Device, 0, len(devices))
	for _, item := range devices {
		if strings.EqualFold(strings.TrimSpace(item.State), "device") {
			connected = append(connected, item)
		}
	}
	if len(connected) == 0 {
		return "", fmt.Errorf("未发现可用的 ADB 设备或模拟器")
	}
	if len(connected) > 1 {
		return "", fmt.Errorf("检测到多个 ADB 设备，请先选择具体序列号")
	}
	return connected[0].Serial, nil
}

func ListAVDs(ctx context.Context) ([]string, error) {
	emulatorPath, err := resolveEmulatorPath()
	if err != nil {
		return nil, err
	}
	return listAVDsWithPath(ctx, emulatorPath)
}

func listAVDsWithPath(ctx context.Context, emulatorPath string) ([]string, error) {
	commandCtx, cancel := context.WithTimeout(ctx, defaultCommandTimeout)
	defer cancel()

	output, err := exec.CommandContext(commandCtx, emulatorPath, "-list-avds").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("emulator -list-avds failed: %w: %s", err, strings.TrimSpace(string(output)))
	}

	lines := strings.Split(string(output), "\n")
	items := make([]string, 0, len(lines))
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		items = append(items, line)
	}
	return items, nil
}

func ResolveAVD(ctx context.Context, requested string) (string, error) {
	name := strings.TrimSpace(requested)
	if name != "" {
		return name, nil
	}
	items, err := ListAVDs(ctx)
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "", fmt.Errorf("未发现可启动的 Android AVD")
	}
	if len(items) > 1 {
		return "", fmt.Errorf("检测到多个 AVD，请先选择具体模拟器")
	}
	return items[0], nil
}

func StartEmulator(avd string) error {
	emulatorPath, err := resolveEmulatorPath()
	if err != nil {
		return err
	}
	resolvedAVD, err := ResolveAVD(context.Background(), avd)
	if err != nil {
		return err
	}

	cmd := exec.Command(emulatorPath, "-avd", resolvedAVD)
	cmd.Env = os.Environ()
	logPath := filepath.Join(os.TempDir(), "mobilevc_emulator_start.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("create emulator startup log failed: %w", err)
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("start emulator failed: %w", err)
	}

	waitCh := make(chan error, 1)
	go func() {
		waitErr := cmd.Wait()
		_ = logFile.Close()
		if waitErr != nil {
			waitCh <- buildEmulatorStartupError(waitErr, resolvedAVD, logPath)
			return
		}
		waitCh <- buildEmulatorStartupError(nil, resolvedAVD, logPath)
	}()

	select {
	case waitErr := <-waitCh:
		return waitErr
	case <-time.After(4 * time.Second):
		return nil
	}
}

func buildEmulatorStartupError(waitErr error, avd, logPath string) error {
	logText := ""
	if data, err := os.ReadFile(logPath); err == nil {
		logText = strings.TrimSpace(string(data))
	}
	if logText != "" {
		lines := strings.Split(logText, "\n")
		for i := len(lines) - 1; i >= 0; i-- {
			line := strings.TrimSpace(lines[i])
			if line == "" {
				continue
			}
			if strings.HasPrefix(line, "ERROR") {
				return fmt.Errorf("启动模拟器 %s 失败：%s", avd, line)
			}
		}
	}
	if waitErr != nil {
		return fmt.Errorf("启动模拟器 %s 失败：%w", avd, waitErr)
	}
	if logText != "" {
		return fmt.Errorf("启动模拟器 %s 后立即退出：%s", avd, lastNonEmptyLine(logText))
	}
	return fmt.Errorf("启动模拟器 %s 后立即退出", avd)
}

func lastNonEmptyLine(text string) string {
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return line
		}
	}
	return ""
}

func DetectStatus(ctx context.Context) Status {
	status := Status{}

	if adbPath, err := resolveADBPath(); err == nil {
		status.ADBAvailable = true
		status.ADBPath = adbPath
		devices, deviceErr := listDevicesWithPath(ctx, adbPath)
		if deviceErr != nil {
			status.Message = deviceErr.Error()
		} else {
			status.Devices = devices
			status.PreferredSerial = preferredSerial(devices)
		}
	} else {
		status.Message = err.Error()
	}

	if emulatorPath, err := resolveEmulatorPath(); err == nil {
		status.EmulatorAvailable = true
		status.EmulatorPath = emulatorPath
		avds, avdErr := listAVDsWithPath(ctx, emulatorPath)
		if avdErr != nil {
			if status.Message == "" {
				status.Message = avdErr.Error()
			}
		} else {
			status.AvailableAVDs = avds
			if len(avds) == 1 {
				status.PreferredAVD = avds[0]
			}
		}
	}

	status.SuggestedAction = suggestedAction(status)
	if status.Message == "" {
		status.Message = defaultStatusMessage(status)
	}
	return status
}

func CaptureFrame(ctx context.Context, serial string) (Frame, error) {
	resolvedSerial, err := ResolveSerial(ctx, serial)
	if err != nil {
		return Frame{}, err
	}
	adbPath, err := resolveADBPath()
	if err != nil {
		return Frame{}, err
	}

	commandCtx, cancel := context.WithTimeout(ctx, frameCommandTimeout)
	defer cancel()

	args := deviceArgs(resolvedSerial, "exec-out", "screencap", "-p")
	output, err := exec.CommandContext(commandCtx, adbPath, args...).Output()
	if err != nil {
		return Frame{}, fmt.Errorf("adb screencap failed: %w", err)
	}
	width, height, err := parsePNGSize(output)
	if err != nil {
		return Frame{}, err
	}
	return Frame{
		Serial:     resolvedSerial,
		Format:     "png",
		Data:       output,
		Width:      width,
		Height:     height,
		CapturedAt: time.Now().UTC(),
	}, nil
}

func Tap(ctx context.Context, serial string, x, y int) error {
	resolvedSerial, err := ResolveSerial(ctx, serial)
	if err != nil {
		return err
	}
	adbPath, err := resolveADBPath()
	if err != nil {
		return err
	}

	commandCtx, cancel := context.WithTimeout(ctx, defaultCommandTimeout)
	defer cancel()

	args := deviceArgs(
		resolvedSerial,
		"shell",
		"input",
		"tap",
		strconv.Itoa(x),
		strconv.Itoa(y),
	)
	output, err := exec.CommandContext(commandCtx, adbPath, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("adb tap failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func Swipe(ctx context.Context, serial string, startX, startY, endX, endY, durationMS int) error {
	resolvedSerial, err := ResolveSerial(ctx, serial)
	if err != nil {
		return err
	}
	adbPath, err := resolveADBPath()
	if err != nil {
		return err
	}
	if durationMS <= 0 {
		durationMS = 220
	}

	commandCtx, cancel := context.WithTimeout(ctx, 2*defaultCommandTimeout)
	defer cancel()

	args := deviceArgs(
		resolvedSerial,
		"shell",
		"input",
		"swipe",
		strconv.Itoa(startX),
		strconv.Itoa(startY),
		strconv.Itoa(endX),
		strconv.Itoa(endY),
		strconv.Itoa(durationMS),
	)
	output, err := exec.CommandContext(commandCtx, adbPath, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("adb swipe failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func Keyevent(ctx context.Context, serial string, keycode string) error {
	resolvedSerial, err := ResolveSerial(ctx, serial)
	if err != nil {
		return err
	}
	adbPath, err := resolveADBPath()
	if err != nil {
		return err
	}
	commandCtx, cancel := context.WithTimeout(ctx, defaultCommandTimeout)
	defer cancel()
	args := deviceArgs(resolvedSerial, "shell", "input", "keyevent", keycode)
	output, err := exec.CommandContext(commandCtx, adbPath, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("adb keyevent %s failed: %w: %s", keycode, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func WarmupScreen(ctx context.Context, serial string) error {
	resolvedSerial, err := ResolveSerial(ctx, serial)
	if err != nil {
		return err
	}
	adbPath, err := resolveADBPath()
	if err != nil {
		return err
	}
	for _, keycode := range []string{"KEYCODE_WAKEUP", "KEYCODE_MENU"} {
		commandCtx, cancel := context.WithTimeout(ctx, defaultCommandTimeout)
		args := deviceArgs(resolvedSerial, "shell", "input", "keyevent", keycode)
		output, runErr := exec.CommandContext(commandCtx, adbPath, args...).CombinedOutput()
		cancel()
		if runErr != nil {
			return fmt.Errorf("adb warmup keyevent %s failed: %w: %s", keycode, runErr, strings.TrimSpace(string(output)))
		}
	}
	return nil
}

func resolveADBPath() (string, error) {
	return resolveBinaryPath("ADB_PATH", "adb", filepath.Join("platform-tools", "adb"))
}

func resolveEmulatorPath() (string, error) {
	return resolveBinaryPath("EMULATOR_PATH", "emulator", filepath.Join("emulator", "emulator"))
}

func resolveBinaryPath(envKey, binaryName, sdkRelativePath string) (string, error) {
	if value := strings.TrimSpace(os.Getenv(envKey)); value != "" {
		if fileExists(value) {
			return value, nil
		}
	}
	if resolved, err := exec.LookPath(binaryName); err == nil {
		return resolved, nil
	}
	for _, root := range sdkRoots() {
		candidate := filepath.Join(root, sdkRelativePath)
		if fileExists(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("未找到 %s，可设置 %s 或安装 Android SDK platform-tools", binaryName, envKey)
}

func sdkRoots() []string {
	seen := map[string]struct{}{}
	items := make([]string, 0, 4)
	add := func(value string) {
		path := strings.TrimSpace(value)
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		items = append(items, path)
	}

	add(os.Getenv("ANDROID_HOME"))
	add(os.Getenv("ANDROID_SDK_ROOT"))
	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, "Library", "Android", "sdk"))
		add(filepath.Join(home, "Android", "Sdk"))
	}
	return items
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func preferredSerial(devices []Device) string {
	connected := make([]Device, 0, len(devices))
	for _, item := range devices {
		if strings.EqualFold(strings.TrimSpace(item.State), "device") {
			connected = append(connected, item)
		}
	}
	if len(connected) == 1 {
		return connected[0].Serial
	}
	return ""
}

func suggestedAction(status Status) string {
	if preferredSerial(status.Devices) != "" {
		return "debug"
	}
	if len(status.AvailableAVDs) > 0 && status.EmulatorAvailable {
		return "start"
	}
	return ""
}

func defaultStatusMessage(status Status) string {
	switch suggestedAction(status) {
	case "debug":
		return "已检测到可调试设备，可直接进入调试。"
	case "start":
		return "未检测到在线设备，可先启动模拟器。"
	default:
		if !status.ADBAvailable && !status.EmulatorAvailable {
			return "未检测到 adb 或 emulator，请确认 Android SDK 已安装。"
		}
		if !status.ADBAvailable {
			return "未检测到 adb，可先安装或配置 Android SDK platform-tools。"
		}
		if !status.EmulatorAvailable {
			return "未检测到 emulator，可先安装 Android Emulator。"
		}
		return "未检测到可调试设备。"
	}
}

func deviceArgs(serial string, args ...string) []string {
	if strings.TrimSpace(serial) == "" {
		return args
	}
	result := make([]string, 0, len(args)+2)
	result = append(result, "-s", strings.TrimSpace(serial))
	result = append(result, args...)
	return result
}

func parsePNGSize(data []byte) (int, int, error) {
	if len(data) < 24 {
		return 0, 0, fmt.Errorf("收到的截图数据不是有效 PNG")
	}
	if string(data[:8]) != "\x89PNG\r\n\x1a\n" {
		return 0, 0, fmt.Errorf("收到的截图数据不是 PNG 格式")
	}
	width := int(binary.BigEndian.Uint32(data[16:20]))
	height := int(binary.BigEndian.Uint32(data[20:24]))
	if width <= 0 || height <= 0 {
		return 0, 0, fmt.Errorf("无法解析截图尺寸")
	}
	return width, height, nil
}
