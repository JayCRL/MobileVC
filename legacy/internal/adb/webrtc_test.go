package adb

import (
	"bytes"
	"testing"
	"time"
)

func TestParseScreenSize(t *testing.T) {
	size, err := parseScreenSize("Physical size: 1080x2400\n")
	if err != nil {
		t.Fatalf("parseScreenSize returned error: %v", err)
	}
	if size.Width != 1080 || size.Height != 2400 {
		t.Fatalf("unexpected size: %+v", size)
	}
}

func TestConstrainSize(t *testing.T) {
	got := constrainSize(Size{Width: 1080, Height: 2400}, 1280)
	if got.Width != 576 || got.Height != 1280 {
		t.Fatalf("unexpected constrained size: %+v", got)
	}
}

func TestStartsNewPicture(t *testing.T) {
	tests := []struct {
		name string
		nal  []byte
		want bool
	}{
		{name: "first slice", nal: []byte{0x41, 0x80}, want: true},
		{name: "non-first slice", nal: []byte{0x41, 0x40}, want: false},
	}

	for _, tt := range tests {
		got, err := startsNewPicture(tt.nal)
		if err != nil {
			t.Fatalf("%s: startsNewPicture returned error: %v", tt.name, err)
		}
		if got != tt.want {
			t.Fatalf("%s: expected %v, got %v", tt.name, tt.want, got)
		}
	}
}

func TestRemoveEmulationPreventionBytes(t *testing.T) {
	got := removeEmulationPreventionBytes([]byte{0x00, 0x00, 0x03, 0x80, 0x00, 0x00, 0x03, 0x40})
	want := []byte{0x00, 0x00, 0x80, 0x00, 0x00, 0x40}
	if !bytes.Equal(got, want) {
		t.Fatalf("unexpected rbsp: got %v want %v", got, want)
	}
}

func TestPumpH264StreamGroupsSlicesIntoFrames(t *testing.T) {
	stream := annexB(
		[]byte{0x67, 0x42, 0x00, 0x1f},
		[]byte{0x68, 0xce, 0x06, 0xe2},
		[]byte{0x41, 0x80},
		[]byte{0x41, 0x40},
		[]byte{0x41, 0x80},
	)

	var frames [][]byte
	var durations []time.Duration
	err := PumpH264Stream(
		t.Context(),
		bytes.NewReader(stream),
		H264StreamConfig{FrameRate: 30},
		func(frame []byte, duration time.Duration) error {
			frames = append(frames, frame)
			durations = append(durations, duration)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("PumpH264Stream returned error: %v", err)
	}
	if len(frames) != 2 {
		t.Fatalf("expected 2 frames, got %d", len(frames))
	}
	if got := countAnnexBNALs(frames[0]); got != 4 {
		t.Fatalf("expected first frame to contain 4 NAL units, got %d", got)
	}
	if got := countAnnexBNALs(frames[1]); got != 1 {
		t.Fatalf("expected second frame to contain 1 NAL unit, got %d", got)
	}
	if durations[0] <= 0 || durations[1] <= 0 {
		t.Fatalf("expected positive durations, got %v", durations)
	}
}

func annexB(nals ...[]byte) []byte {
	var stream []byte
	for _, nal := range nals {
		stream = append(stream, 0x00, 0x00, 0x00, 0x01)
		stream = append(stream, nal...)
	}
	return stream
}

func countAnnexBNALs(frame []byte) int {
	count := 0
	for index := 0; index+3 < len(frame); index++ {
		if frame[index] == 0x00 &&
			frame[index+1] == 0x00 &&
			frame[index+2] == 0x00 &&
			frame[index+3] == 0x01 {
			count++
		}
	}
	return count
}
