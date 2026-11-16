package engine

import (
	"bufio"
	"errors"
	"fmt"
	"io"
)

const runnerMaxLineBytes = 16 * 1024 * 1024

func forEachLine(reader io.Reader, fn func([]byte) error) error {
	buffered := bufio.NewReaderSize(reader, 64*1024)
	line := make([]byte, 0, 64*1024)

	flush := func() error {
		if len(line) == 0 {
			return nil
		}
		if err := fn(line); err != nil {
			return err
		}
		line = line[:0]
		return nil
	}

	for {
		fragment, isPrefix, err := buffered.ReadLine()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return flush()
			}
			return err
		}
		if len(line)+len(fragment) > runnerMaxLineBytes {
			return fmt.Errorf("line too long: exceeded %d bytes", runnerMaxLineBytes)
		}
		line = append(line, fragment...)
		if isPrefix {
			continue
		}
		if err := flush(); err != nil {
			return err
		}
	}
}
