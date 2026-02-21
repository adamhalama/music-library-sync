package output

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

type EventEmitter interface {
	Emit(event Event) error
}

type JSONEmitter struct {
	enc *json.Encoder
	mu  sync.Mutex
}

func NewJSONEmitter(w io.Writer) *JSONEmitter {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return &JSONEmitter{enc: enc}
}

func (e *JSONEmitter) Emit(event Event) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.enc.Encode(event)
}

type HumanEmitter struct {
	stdout  io.Writer
	stderr  io.Writer
	quiet   bool
	verbose bool
}

func NewHumanEmitter(stdout, stderr io.Writer, quiet, verbose bool) *HumanEmitter {
	return &HumanEmitter{stdout: stdout, stderr: stderr, quiet: quiet, verbose: verbose}
}

func (e *HumanEmitter) Emit(event Event) error {
	line := event.Message
	if line == "" {
		line = string(event.Event)
	}

	switch event.Level {
	case LevelError:
		_, err := fmt.Fprintln(e.stderr, "ERROR:", line)
		return err
	case LevelWarn:
		if e.quiet {
			return nil
		}
		_, err := fmt.Fprintln(e.stderr, "WARN:", line)
		return err
	default:
		if e.quiet && event.Event != EventSyncFinished {
			return nil
		}
		if !e.verbose && event.Event == EventSourceStarted {
			return nil
		}
		_, err := fmt.Fprintln(e.stdout, line)
		return err
	}
}

type MultiEmitter struct {
	emitters []EventEmitter
}

func NewMultiEmitter(emitters ...EventEmitter) *MultiEmitter {
	return &MultiEmitter{emitters: emitters}
}

func (e *MultiEmitter) Emit(event Event) error {
	for _, emitter := range e.emitters {
		if err := emitter.Emit(event); err != nil {
			return err
		}
	}
	return nil
}
