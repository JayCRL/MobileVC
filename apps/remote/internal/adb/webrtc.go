package adb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pion/webrtc/v4/pkg/media/h264reader"
)

const (
	defaultH264BitRate      = 2_000_000
	defaultH264MaxDimension = 1280
	defaultH264TimeLimit    = 170 * time.Second
	defaultH264FrameRate    = 30
)

var screenSizePattern = regexp.MustCompile(`(\d+)x(\d+)`)

type Size struct {
	Width  int
	Height int
}

type H264StreamConfig struct {
	BitRate      int
	MaxDimension int
	TimeLimit    time.Duration
	FrameRate    int
}

type H264Stream struct {
	Serial     string
	ScreenSize Size
	StreamSize Size
	stdout     io.ReadCloser
	waitCh     chan error
	cancel     context.CancelFunc
}

func ResolveScreenSize(ctx context.Context, serial string) (Size, error) {
	resolvedSerial, err := ResolveSerial(ctx, serial)
	if err != nil {
		return Size{}, err
	}
	adbPath, err := resolveADBPath()
	if err != nil {
		return Size{}, err
	}

	commandCtx, cancel := context.WithTimeout(ctx, defaultCommandTimeout)
	defer cancel()

	output, err := exec.CommandContext(commandCtx, adbPath, deviceArgs(resolvedSerial, "shell", "wm", "size")...).CombinedOutput()
	if err != nil {
		return Size{}, fmt.Errorf("adb wm size failed: %w: %s", err, strings.TrimSpace(string(output)))
	}

	size, parseErr := parseScreenSize(string(output))
	if parseErr != nil {
		return Size{}, parseErr
	}
	return size, nil
}

func StartH264Stream(ctx context.Context, serial string, cfg H264StreamConfig) (*H264Stream, error) {
	resolvedSerial, err := ResolveSerial(ctx, serial)
	if err != nil {
		return nil, err
	}
	screenSize, err := ResolveScreenSize(ctx, resolvedSerial)
	if err != nil {
		return nil, err
	}
	adbPath, err := resolveADBPath()
	if err != nil {
		return nil, err
	}

	normalized := normalizeH264Config(cfg)
	streamSize := constrainSize(screenSize, normalized.MaxDimension)
	commandCtx, cancel := context.WithCancel(ctx)

	args := deviceArgs(
		resolvedSerial,
		"exec-out",
		"screenrecord",
		"--output-format=h264",
		"--bit-rate", strconv.Itoa(normalized.BitRate),
		"--time-limit", strconv.Itoa(int(normalized.TimeLimit.Seconds())),
	)
	if streamSize.Width > 0 && streamSize.Height > 0 {
		args = append(args, "--size", fmt.Sprintf("%dx%d", streamSize.Width, streamSize.Height))
	}
	args = append(args, "-")

	cmd := exec.CommandContext(commandCtx, adbPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("prepare screenrecord stdout failed: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start adb screenrecord failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	waitCh := make(chan error, 1)
	go func() {
		waitErr := cmd.Wait()
		if waitErr != nil && strings.TrimSpace(stderr.String()) != "" {
			waitErr = fmt.Errorf("%w: %s", waitErr, strings.TrimSpace(stderr.String()))
		}
		waitCh <- waitErr
		close(waitCh)
	}()

	return &H264Stream{
		Serial:     resolvedSerial,
		ScreenSize: screenSize,
		StreamSize: streamSize,
		stdout:     stdout,
		waitCh:     waitCh,
		cancel:     cancel,
	}, nil
}

func (s *H264Stream) Reader() io.Reader {
	if s == nil {
		return nil
	}
	return s.stdout
}

func (s *H264Stream) Close() error {
	if s == nil {
		return nil
	}
	if s.cancel != nil {
		s.cancel()
	}
	if s.stdout != nil {
		_ = s.stdout.Close()
	}
	var err error
	if s.waitCh != nil {
		err = <-s.waitCh
	}
	return err
}

func PumpH264Stream(ctx context.Context, reader io.Reader, cfg H264StreamConfig, emit func([]byte, time.Duration) error) error {
	if reader == nil {
		return fmt.Errorf("h264 reader is nil")
	}
	if emit == nil {
		return fmt.Errorf("h264 emitter is nil")
	}
	normalized := normalizeH264Config(cfg)
	h264, err := h264reader.NewReader(reader)
	if err != nil {
		return fmt.Errorf("create h264 reader failed: %w", err)
	}

	frameDuration := time.Second / time.Duration(normalized.FrameRate)
	minDuration := frameDuration / 2
	if minDuration < time.Second/120 {
		minDuration = time.Second / 120
	}
	maxDuration := frameDuration * 3
	lastEmitAt := time.Time{}
	sample := make([]byte, 0, 128*1024)
	hasVCL := false
	flush := func() error {
		if !hasVCL || len(sample) == 0 {
			sample = sample[:0]
			hasVCL = false
			return nil
		}
		duration := frameDuration
		now := time.Now()
		if !lastEmitAt.IsZero() {
			observed := now.Sub(lastEmitAt)
			switch {
			case observed < minDuration:
				duration = minDuration
			case observed > maxDuration:
				duration = maxDuration
			default:
				duration = observed
			}
		}
		lastEmitAt = now
		frame := append([]byte(nil), sample...)
		sample = sample[:0]
		hasVCL = false
		return emit(frame, duration)
	}

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		nal, err := h264.NextNAL()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return flush()
			}
			return err
		}
		if nal == nil || len(nal.Data) == 0 {
			continue
		}

		switch nal.UnitType {
		case h264reader.NalUnitTypeAUD, h264reader.NalUnitTypeEndOfSequence, h264reader.NalUnitTypeEndOfStream:
			if err := flush(); err != nil {
				return err
			}
			continue
		case h264reader.NalUnitTypeCodedSliceIdr, h264reader.NalUnitTypeCodedSliceNonIdr:
			startsPicture, err := startsNewPicture(nal.Data)
			if err != nil {
				return fmt.Errorf("parse h264 slice header failed: %w", err)
			}
			if hasVCL && startsPicture {
				if err := flush(); err != nil {
					return err
				}
			}
			sample = appendAnnexB(sample, nal.Data)
			hasVCL = true
		default:
			if hasVCL {
				if err := flush(); err != nil {
					return err
				}
			}
			if len(sample) == 0 &&
				nal.UnitType != h264reader.NalUnitTypeSPS &&
				nal.UnitType != h264reader.NalUnitTypePPS &&
				nal.UnitType != h264reader.NalUnitTypeSEI {
				continue
			}
			sample = appendAnnexB(sample, nal.Data)
		}
	}
}

func startsNewPicture(nal []byte) (bool, error) {
	if len(nal) <= 1 {
		return false, fmt.Errorf("slice nal is too short")
	}
	firstMBInSlice, err := readUE(removeEmulationPreventionBytes(nal[1:]))
	if err != nil {
		return false, err
	}
	return firstMBInSlice == 0, nil
}

func removeEmulationPreventionBytes(data []byte) []byte {
	if len(data) < 3 {
		return append([]byte(nil), data...)
	}
	result := make([]byte, 0, len(data))
	zeroRun := 0
	for _, b := range data {
		if zeroRun >= 2 && b == 0x03 {
			zeroRun = 0
			continue
		}
		result = append(result, b)
		if b == 0 {
			zeroRun++
		} else {
			zeroRun = 0
		}
	}
	return result
}

func readUE(data []byte) (uint, error) {
	var (
		bitIndex int
		zeros    int
	)
	for {
		bit, err := readBit(data, &bitIndex)
		if err != nil {
			return 0, err
		}
		if bit == 1 {
			break
		}
		zeros++
	}
	if zeros == 0 {
		return 0, nil
	}
	suffix := uint(0)
	for i := 0; i < zeros; i++ {
		bit, err := readBit(data, &bitIndex)
		if err != nil {
			return 0, err
		}
		suffix = (suffix << 1) | uint(bit)
	}
	return uint((1<<zeros)-1) + suffix, nil
}

func readBit(data []byte, bitIndex *int) (byte, error) {
	if *bitIndex >= len(data)*8 {
		return 0, io.ErrUnexpectedEOF
	}
	byteIndex := *bitIndex / 8
	shift := 7 - (*bitIndex % 8)
	*bitIndex = *bitIndex + 1
	return (data[byteIndex] >> shift) & 0x01, nil
}

func parseScreenSize(output string) (Size, error) {
	match := screenSizePattern.FindStringSubmatch(output)
	if len(match) != 3 {
		return Size{}, fmt.Errorf("无法解析设备分辨率: %s", strings.TrimSpace(output))
	}
	width, err := strconv.Atoi(match[1])
	if err != nil {
		return Size{}, fmt.Errorf("parse screen width failed: %w", err)
	}
	height, err := strconv.Atoi(match[2])
	if err != nil {
		return Size{}, fmt.Errorf("parse screen height failed: %w", err)
	}
	if width <= 0 || height <= 0 {
		return Size{}, fmt.Errorf("非法设备分辨率: %dx%d", width, height)
	}
	return Size{Width: width, Height: height}, nil
}

func constrainSize(size Size, maxDimension int) Size {
	if size.Width <= 0 || size.Height <= 0 {
		return Size{}
	}
	if maxDimension <= 0 {
		return evenSize(size)
	}
	longest := size.Width
	if size.Height > longest {
		longest = size.Height
	}
	if longest <= maxDimension {
		return evenSize(size)
	}

	scale := float64(maxDimension) / float64(longest)
	width := int(float64(size.Width) * scale)
	height := int(float64(size.Height) * scale)
	if width < 2 {
		width = 2
	}
	if height < 2 {
		height = 2
	}
	return evenSize(Size{Width: width, Height: height})
}

func evenSize(size Size) Size {
	width := size.Width
	height := size.Height
	if width%2 != 0 {
		width--
	}
	if height%2 != 0 {
		height--
	}
	if width < 2 {
		width = 2
	}
	if height < 2 {
		height = 2
	}
	return Size{Width: width, Height: height}
}

func appendAnnexB(dst, nal []byte) []byte {
	dst = append(dst, 0x00, 0x00, 0x00, 0x01)
	return append(dst, nal...)
}

func normalizeH264Config(cfg H264StreamConfig) H264StreamConfig {
	if cfg.BitRate <= 0 {
		cfg.BitRate = defaultH264BitRate
	}
	if cfg.MaxDimension <= 0 {
		cfg.MaxDimension = defaultH264MaxDimension
	}
	if cfg.TimeLimit <= 0 {
		cfg.TimeLimit = defaultH264TimeLimit
	}
	if cfg.FrameRate <= 0 {
		cfg.FrameRate = defaultH264FrameRate
	}
	return cfg
}
