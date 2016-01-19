package process

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/albertrdixon/gearbox/logger"

	"golang.org/x/net/context"
)

type Process struct {
	*exec.Cmd
	name   string
	ctx    context.Context
	out    io.Writer
	exited chan struct{}
}

func New(name, cmd string, out io.Writer, c context.Context) (*Process, error) {
	list := strings.Fields(cmd)
	if len(list) < 1 {
		return nil, errors.New("Bad command")
	}

	path, er := exec.LookPath(list[0])
	if er != nil {
		return nil, er
	}
	return &Process{
		Cmd:    exec.Command(path, list[1:]...),
		name:   name,
		ctx:    c,
		out:    out,
		exited: make(chan struct{}, 1),
	}, nil
}

func (p *Process) String() string {
	pid := -1
	if p.Process != nil {
		pid = p.Process.Pid
	}
	return fmt.Sprintf("%s(pid=%d)", p.name, pid)
}

func (p *Process) Exited() <-chan struct{} {
	return p.exited
}

func (p *Process) Execute() error {
	sto, er := p.StdoutPipe()
	if er != nil {
		return er
	}
	ste, er := p.StderrPipe()
	if er != nil {
		return er
	}

	go stream(p.name, sto, p.out, p.ctx)
	go stream(p.name, ste, p.out, p.ctx)

	if er := p.Start(); er != nil {
		return er
	}
	go monitor(p)

	go func() {
		defer p.Process.Release()
		select {
		case <-p.exited:
			return
		case <-p.ctx.Done():
			p.Stop()
		}
	}()

	return nil
}

func (p *Process) SetUser(uid, gid int) {
	p.Cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: uint32(uid),
			Gid: uint32(gid),
		},
	}
}

func (p *Process) Stop() error {
	if p.ProcessState != nil && !p.ProcessState.Exited() {
		logger.Debugf("Sending SIGTERM to %s", p.name)
		if er := p.Process.Signal(syscall.SIGTERM); er != nil {
			return er
		}
		time.Sleep(100 * time.Millisecond)
		if p.ProcessState != nil && !p.ProcessState.Exited() {
			logger.Debugf("SIGTERM to %s failed, killing", p.name)
			if er := p.Process.Kill(); er != nil {
				return er
			}
		}
		p.Process.Release()
	}
	return nil
}

func stream(name string, r io.Reader, w io.Writer, c context.Context) {
	s := bufio.NewScanner(r)
	for s.Scan() {
		select {
		case <-c.Done():
			return
		default:
			fmt.Fprintf(w, "[%s] %s\n", name, s.Text())
		}
	}
}

func monitor(p *Process) {
	defer close(p.exited)
	t := time.NewTicker(5 * time.Second)
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-t.C:
			if p.ProcessState != nil && p.ProcessState.Exited() {
				p.exited <- struct{}{}
				return
			}

			pid := p.Process.Pid
			if _, er := os.FindProcess(pid); er != nil {
				p.exited <- struct{}{}
				return
			}
		}
	}
}
