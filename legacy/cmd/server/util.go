package main

func fallback(value, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}
