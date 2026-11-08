package supervisor

import (
	"errors"
	"log"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// stopTimeout is below what the container's stop allows, which is 120 seconds.
const stopTimeout = 90 * time.Second

// process is a long-running child.
type process struct {
	name    string
	command *exec.Cmd
	// done closes when it has ended, and err then says how.
	done chan struct{}
	err  error
}

func startProcess(name string, command *exec.Cmd) (*process, error) {
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	if err := command.Start(); err != nil {
		return nil, err
	}
	started := &process{name: name, command: command, done: make(chan struct{})}
	go func() {
		started.err = command.Wait()
		close(started.done)
	}()
	return started, nil
}

func (p *process) running() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

// stop asks a process to end, and kills it if it doesn't.
func (p *process) stop(signal syscall.Signal) {
	if err := p.command.Process.Signal(signal); err != nil && !errors.Is(err, os.ErrProcessDone) {
		log.Printf("error: stopping %s: %v", p.name, err)
	}
	select {
	case <-p.done:
	case <-time.After(stopTimeout):
		log.Printf("error: %s did not stop in time", p.name)
		_ = p.command.Process.Kill()
		<-p.done
	}
}
