package pkgstore

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/andrewchambers/magnet-linux/magpkg/pkggraph"
)

func TestBuildOutputWriterPrefixesCompleteAndPartialLines(t *testing.T) {
	var output bytes.Buffer
	outputMutex := &sync.Mutex{}
	writer := newBuildOutputWriter(
		&output,
		outputMutex,
		"[hello abcdef01] ",
	)

	for _, chunk := range []string{
		"first",
		" line\nsecond\r\nthird\r",
		"\nfourth",
	} {
		if _, err := writer.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}

	want := "" +
		"[hello abcdef01] first line\n" +
		"[hello abcdef01] second\n" +
		"[hello abcdef01] third\n" +
		"[hello abcdef01] fourth\n"
	if output.String() != want {
		t.Fatalf("output:\n%s\nwant:\n%s", output.String(), want)
	}
}

func TestBuildOutputWritersDoNotInterleaveLines(t *testing.T) {
	var output bytes.Buffer
	outputMutex := &sync.Mutex{}
	first := newBuildOutputWriter(&output, outputMutex, "[first 11111111] ")
	second := newBuildOutputWriter(&output, outputMutex, "[second 22222222] ")

	var writers sync.WaitGroup
	for name, writer := range map[string]*buildOutputWriter{
		"first":  first,
		"second": second,
	} {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for index := range 100 {
				line := fmt.Sprintf("%s-%03d\n", name, index)
				if _, err := writer.Write([]byte(line)); err != nil {
					t.Errorf("write: %v", err)
					return
				}
			}
		}()
	}
	writers.Wait()

	lines := strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n")
	if len(lines) != 200 {
		t.Fatalf("line count = %d, want 200", len(lines))
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "[first 11111111] first-") ||
			strings.HasPrefix(line, "[second 22222222] second-") {
			continue
		}
		t.Fatalf("interleaved or incorrectly prefixed line %q", line)
	}
}

func TestBuildOutputWriterBoundsLinesWithoutAddingBlankLine(t *testing.T) {
	var output bytes.Buffer
	writer := newBuildOutputWriter(
		&output,
		&sync.Mutex{},
		"[package 12345678] ",
	)
	line := strings.Repeat("x", maxBuildOutputLine)
	if _, err := writer.Write([]byte(line + "\n")); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "[package 12345678] "+line+"\n"; got != want {
		t.Fatalf("bounded output has length %d, want %d", len(got), len(want))
	}
}

func TestBuildOutputPrefixUsesNameAndShortID(t *testing.T) {
	packageID := pkggraph.ID(strings.Repeat("a", pkggraph.IDLength))
	if got, want := buildOutputPrefix(PlannedPackage{
		ID:         packageID,
		Definition: pkggraph.Definition{Name: "hello", Build: pkggraph.BuildSpec{Kind: pkggraph.BuildNone}},
	}), "[hello aaaaaaaa] "; got != want {
		t.Fatalf("named prefix = %q, want %q", got, want)
	}
	if got, want := buildOutputPrefix(PlannedPackage{
		ID: packageID,
	}), "[aaaaaaaa] "; got != want {
		t.Fatalf("unnamed prefix = %q, want %q", got, want)
	}
}
