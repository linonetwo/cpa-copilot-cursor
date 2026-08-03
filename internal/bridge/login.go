package bridge

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

var (
	loginURLPattern = regexp.MustCompile(`https://[^\s<>"']+`)
	userCodePattern = regexp.MustCompile(`\b[A-Z0-9]{4}-[A-Z0-9]{4}\b`)
)

type loginFlow struct {
	provider string
	handle   string
	command  *exec.Cmd
	terminal *os.File

	mu       sync.Mutex
	output   []string
	url      string
	userCode string
	waitErr  error
	finished bool
	done     chan struct{}
}

func startLoginFlow(provider, handle string, command *exec.Cmd) (*loginFlow, error) {
	terminal, err := pty.Start(command)
	if err != nil {
		return nil, err
	}
	flow := &loginFlow{
		provider: provider,
		handle:   handle,
		command:  command,
		terminal: terminal,
		done:     make(chan struct{}),
	}
	go flow.readOutput()
	return flow, nil
}

func (f *loginFlow) readOutput() {
	defer close(f.done)
	defer f.terminal.Close()
	buffer := make([]byte, 4096)
	pending := ""
	for {
		count, err := f.terminal.Read(buffer)
		if count > 0 {
			pending += string(buffer[:count])
			lines := strings.SplitAfter(pending, "\n")
			pending = ""
			if len(lines) > 0 && !strings.HasSuffix(lines[len(lines)-1], "\n") {
				pending = lines[len(lines)-1]
				lines = lines[:len(lines)-1]
			}
			for _, line := range lines {
				f.record(line)
			}
			f.recordMarkers(pending)
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, syscall.EIO) {
				f.mu.Lock()
				f.waitErr = err
				f.mu.Unlock()
			}
			break
		}
	}
	if pending != "" {
		f.record(pending)
	}
	waitErr := f.command.Wait()
	f.mu.Lock()
	if waitErr != nil {
		f.waitErr = waitErr
	}
	f.finished = true
	f.mu.Unlock()
}

func (f *loginFlow) record(value string) {
	text := strings.TrimSpace(value)
	if text == "" {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.output = append(f.output, text)
	f.recordMarkersLocked(text)
}

func (f *loginFlow) recordMarkers(value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordMarkersLocked(value)
}

func (f *loginFlow) recordMarkersLocked(value string) {
	if f.url == "" {
		if match := loginURLPattern.FindString(value); match != "" {
			f.url = strings.TrimRight(match, ".,;)")
		}
	}
	if f.userCode == "" {
		f.userCode = userCodePattern.FindString(value)
	}
}

func (f *loginFlow) snapshot() (url, userCode, recent string, finished bool, waitErr error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	start := len(f.output) - 12
	if start < 0 {
		start = 0
	}
	recent = strings.Join(f.output[start:], "\n")
	if len(recent) > 4000 {
		recent = recent[len(recent)-4000:]
	}
	return f.url, f.userCode, recent, f.finished, f.waitErr
}

func (f *loginFlow) stop() {
	if f.command.Process != nil {
		_ = f.command.Process.Signal(os.Interrupt)
		select {
		case <-f.done:
			return
		case <-time.After(3 * time.Second):
			_ = f.command.Process.Kill()
		}
	}
	<-f.done
}
