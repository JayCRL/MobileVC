package adb

import "testing"

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
