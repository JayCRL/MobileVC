package relay

type codeError struct {
	code    string
	message string
}

func newCodeError(code string, message string) error {
	return codeError{code: code, message: message}
}

func (e codeError) Error() string {
	return e.message
}

func errorCode(err error, fallback string) string {
	if coded, ok := err.(codeError); ok {
		return coded.code
	}
	return fallback
}
