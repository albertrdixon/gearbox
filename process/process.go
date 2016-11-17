package process

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/context"
)

type Process struct {
	*exec.Cmd
	attr           *syscall.SysProcAttr
	name, bin, dir string
	rawOut, tty    bool
	args, env      []string
	c              context.Context
	out            []io.Writer
	stdin          io.Reader
	stopC          chan struct{}
	er             error
}

func New(name, cmd string, out ...io.Writer) (*Process, error) {
	fields := strings.Fields(cmd)
	if len(fields) < 1 {
		return nil, errors.New("Bad command")
	}

	bin, er := exec.LookPath(fields[0])
	if er != nil {
		return nil, er
	}

	return &Process{
		name:   name,
		bin:    bin,
		args:   fields[1:],
		tty:    false,
		out:    out,
		rawOut: false,
		stdin:  nil,
		er:     nil,
	}, nil
}

func (p *Process) String() string {
	pid := p.Pid()
	if pid == -1 {
		return fmt.Sprint(p.name)
	}
	return fmt.Sprintf("%s(pid=%d)", p.name, pid)
}

func (p *Process) AddWriter(w io.Writer) *Process {
	if p.out == nil {
		p.out = make([]io.Writer, 0, 1)
	}
	p.out = append(p.out, w)
	return p
}

func (p *Process) SetStdin(r io.Reader) *Process {
	p.stdin = r
	return p
}

func (p *Process) SetDir(dir string) *Process {
	p.dir = dir
	return p
}

func (p *Process) SetEnv(env []string) *Process {
	p.env = env
	return p
}

func (p *Process) SetUser(uid, gid uint32) *Process {
	p.attr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: uid,
			Gid: gid,
		},
	}
	return p
}

func (p *Process) Pid() int {
	if p.Cmd != nil && p.Cmd.Process != nil {
		return p.Process.Pid
	}
	return -1
}

func (p *Process) Exited() <-chan struct{} {
	return p.c.Done()
}

func (p *Process) Execute(ctx context.Context) error {
	p.stopC = make(chan struct{}, 1)
	p.Cmd = exec.Command(p.bin, p.args...)
	if p.attr != nil {
		p.Cmd.SysProcAttr = p.attr
	}
	if p.dir != "" {
		p.Cmd.Dir = p.dir
	}
	if len(p.env) > 0 {
		p.Cmd.Env = p.env
	}

	c, cancel := context.WithCancel(context.Background())
	p.c = c

	if p.out != nil {
		sto, er := p.StdoutPipe()
		if er != nil {
			cancel()
			return er
		}
		ste, er := p.StderrPipe()
		if er != nil {
			cancel()
			return er
		}

		go stream(p, sto)
		go stream(p, ste)
	} else {
		p.Stdout = nil
		p.Stderr = nil
	}

	if p.tty {
		p.Stdin = os.Stdin
	} else if p.stdin != nil {
		sti, er := p.StdinPipe()
		if er != nil {
			cancel()
			return er
		}
		go toStdin(p, sti)
	}

	if er := p.Start(); er != nil {
		cancel()
		return er
	}

	go listen(p, ctx)
	go wait(p, cancel)

	return nil
}

func (p *Process) ExecuteAndRestart(ctx context.Context) {
	for {
		if er := p.Execute(ctx); er != nil {
			p.er = er
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-p.stopC:
			return
		case <-p.c.Done():
		}
	}
}

func (p *Process) RawOutput() *Process {
	p.rawOut = true
	return p
}

func (p *Process) MakeInteractive() *Process {
	p.RawOutput()
	p.tty = true
	return p
}

func (p *Process) Stop() {
	p.stopC <- struct{}{}
	close(p.stopC)
}

func (p *Process) Release() error {
	if p.Process != nil {
		return p.Process.Release()
	}
	return nil
}

func (p *Process) Dead() bool {
	return p.ProcessState != nil && p.ProcessState.Exited()
}

func (p *Process) Kill() error {
	if p.Process != nil {
		return p.Process.Kill()
	}
	return nil
}

func (p *Process) Signal(sig os.Signal) error {
	if p.Process != nil {
		return p.Process.Signal(sig)
	}
	return nil
}

func (p *Process) Term() error {
	if p.Process != nil {
		if er := p.Signal(syscall.SIGTERM); er != nil {
			return er
		}
		time.Sleep(20 * time.Millisecond)
		if p.ProcessState == nil {
			return p.Kill()
		}
	}
	return nil
}

func (p *Process) Error() error {
	return p.er
}

func stream(p *Process, r io.Reader) {
	s := bufio.NewScanner(r)
	for {
		select {
		case <-p.c.Done():
			return
		default:
			if s.Scan() {
				txt := s.Text()
				for _, w := range p.out {
					if p.rawOut {
						fmt.Fprintln(w, txt)
					} else {
						fmt.Fprintf(w, "[%s] %s\n", p.name, txt)
					}
				}
			} else {
				return
			}
		}
	}
	if s.Err() != nil {
		log.Printf("[error] %v stream error: %v", p, s.Err())
	}
}

func listen(p *Process, ctx context.Context) {
	select {
	case <-p.c.Done():
		return
	case <-ctx.Done():
		p.Kill()
	case <-p.stopC:
		p.Term()
	}
}

func wait(p *Process, cancel context.CancelFunc) {
	p.er = p.Wait()
	log.Printf("[debug] %v exited", p)
	cancel()
}

func toStdin(p *Process, dst io.Writer) {
	select {
	case <-p.c.Done():
		return
	default:
		io.Copy(dst, p.stdin)
	}
}
