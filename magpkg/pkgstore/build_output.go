package pkgstore

import (
	"errors"
	"io"
	"sync"
)

const (
	buildOutputIDLength = 8
	maxBuildOutputLine  = 64 * 1024
)

// buildOutputWriter turns a command byte stream into complete, prefixed lines.
// outputMutex is shared by every package in one executor so concurrent builds
// cannot interleave within a line.
type buildOutputWriter struct {
	mutex       sync.Mutex
	outputMutex *sync.Mutex
	output      io.Writer
	prefix      []byte
	pending     []byte
	skipLF      bool
	chunkedLine bool
}

func newBuildOutputWriter(
	output io.Writer,
	outputMutex *sync.Mutex,
	prefix string,
) *buildOutputWriter {
	if outputMutex == nil {
		outputMutex = &sync.Mutex{}
	}
	return &buildOutputWriter{
		outputMutex: outputMutex,
		output:      output,
		prefix:      []byte(prefix),
	}
}

func (writer *buildOutputWriter) Write(data []byte) (int, error) {
	writer.mutex.Lock()
	defer writer.mutex.Unlock()

	for index, character := range data {
		if writer.skipLF {
			writer.skipLF = false
			if character == '\n' {
				continue
			}
		}
		switch character {
		case '\n', '\r':
			if len(writer.pending) > 0 || !writer.chunkedLine {
				if err := writer.writePendingLine(); err != nil {
					return index + 1, err
				}
			}
			writer.chunkedLine = false
			writer.skipLF = character == '\r'
		default:
			writer.pending = append(writer.pending, character)
			if len(writer.pending) >= maxBuildOutputLine {
				if err := writer.writePendingLine(); err != nil {
					return index + 1, err
				}
				writer.chunkedLine = true
			}
		}
	}
	return len(data), nil
}

func (writer *buildOutputWriter) Flush() error {
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	if len(writer.pending) == 0 {
		return nil
	}
	if err := writer.writePendingLine(); err != nil {
		return err
	}
	writer.chunkedLine = false
	return nil
}

func (writer *buildOutputWriter) writePendingLine() error {
	line := make([]byte, 0, len(writer.prefix)+len(writer.pending)+1)
	line = append(line, writer.prefix...)
	line = append(line, writer.pending...)
	line = append(line, '\n')

	writer.outputMutex.Lock()
	defer writer.outputMutex.Unlock()
	if err := writeFull(writer.output, line); err != nil {
		return err
	}
	writer.pending = writer.pending[:0]
	return nil
}

func writeFull(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if written < 0 || written > len(data) {
			return errors.New("pkgstore: invalid build output write count")
		}
		data = data[written:]
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func buildOutputPrefix(planned PlannedPackage) string {
	shortID := string(planned.ID)
	if len(shortID) > buildOutputIDLength {
		shortID = shortID[:buildOutputIDLength]
	}
	if planned.Definition.Name == "" {
		return "[" + shortID + "] "
	}
	return "[" + planned.Definition.Name + " " + shortID + "] "
}
