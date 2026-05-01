package engine

import (
	"regexp"
)

var ansiPattern = regexp.MustCompile(`(?:\x1B\[[0-?]*[ -/]*[@-~]|\x1B\][^\x07\x1B]*(?:\x07|\x1B\\)|\x1B[@-_]|[\x00-\x08\x0B\x0C\x0E-\x1F\x7F])`)

func StripANSI(str string) string {
	return ansiPattern.ReplaceAllString(str, "")
}

func StripANSIChunk(chunk, carry string) (cleaned string, nextCarry string) {
	combined := carry + chunk
	if combined == "" {
		return "", ""
	}
	safe, trailing := splitANSITrailingCarry(combined)
	return StripANSI(safe), trailing
}

func splitANSITrailingCarry(value string) (safe string, trailing string) {
	for i := len(value) - 1; i >= 0; i-- {
		if value[i] != 0x1b {
			continue
		}
		if hasIncompleteANSISuffix(value[i:]) {
			return value[:i], value[i:]
		}
		break
	}
	return value, ""
}

func hasIncompleteANSISuffix(suffix string) bool {
	if suffix == "" || suffix[0] != 0x1b {
		return false
	}
	if len(suffix) == 1 {
		return true
	}

	switch suffix[1] {
	case '[':
		return isIncompleteCSI(suffix[2:])
	case ']':
		return isIncompleteOSC(suffix[2:])
	default:
		return false
	}
}

func isIncompleteCSI(body string) bool {
	for i := 0; i < len(body); i++ {
		b := body[i]
		switch {
		case b >= 0x30 && b <= 0x3f:
		case b >= 0x20 && b <= 0x2f:
		case b >= 0x40 && b <= 0x7e:
			return false
		default:
			return false
		}
	}
	return true
}

func isIncompleteOSC(body string) bool {
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case 0x07:
			return false
		case 0x1b:
			if i+1 >= len(body) {
				return true
			}
			return body[i+1] != '\\'
		}
	}
	return true
}
