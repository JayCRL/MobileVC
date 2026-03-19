package adapter

import "regexp"

var ansiPattern = regexp.MustCompile(`(?:\x1B\[[0-?]*[ -/]*[@-~]|\x1B\][^\x07\x1B]*(?:\x07|\x1B\\)|\x1B[@-_]|[\x00-\x08\x0B\x0C\x0E-\x1F\x7F])`)

func StripANSI(str string) string {
	return ansiPattern.ReplaceAllString(str, "")
}
