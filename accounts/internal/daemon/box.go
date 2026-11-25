package daemon

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/boxmcp"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

// The box socket's lifetime, workload and filesystem limits.
const (
	BoxTimeout              = 15 * time.Second
	MaxBoxConns             = 8
	SocketMode  fs.FileMode = 0o666
)

// Refusals sent to clients and diagnostics written to the daemon's stderr.
const (
	NotActiveUser = "your Linux user is not an active user of an account on this host"
	CannotReach   = "the host cannot reach accounts right now; try again shortly"
	DiagSocket    = "box socket failed"
	DiagForward   = "box request failed"
)

// KernelUID returns a Unix stream peer's uid at the time it connected.
func KernelUID(c net.Conn) (int64, error) {
	unix, ok := c.(*net.UnixConn)
	if !ok {
		return 0, errors.New("peer credentials require a Unix connection")
	}
	raw, err := unix.SyscallConn()
	if err != nil {
		return 0, err
	}
	var cred *syscall.Ucred
	var sockErr error
	err = raw.Control(func(fd uintptr) {
		cred, sockErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	})
	if err != nil {
		return 0, err
	}
	if sockErr != nil {
		return 0, sockErr
	}
	return int64(cred.Uid), nil
}

func (r *reconciler) serveBox(acceptCtx, ctx context.Context) {
	var connections sync.WaitGroup
	defer connections.Wait()
	slots := make(chan struct{}, MaxBoxConns)
	for acceptCtx.Err() == nil {
		conn, err := r.cfg.Box.Accept()
		if err != nil {
			if acceptCtx.Err() != nil {
				return
			}
			r.diagnose(DiagSocket, err)
			if errors.Is(err, net.ErrClosed) {
				return
			}
			r.cfg.Sleep(acceptCtx, MinBackoff)
			continue
		}
		timeout := r.cfg.After(BoxTimeout)
		select {
		case slots <- struct{}{}:
			connections.Go(func() {
				defer func() { <-slots }()
				r.serveBoxConn(ctx, conn, timeout, false)
			})
		default:
			r.serveBoxConn(ctx, conn, timeout, true)
		}
	}
}

func (r *reconciler) serveBoxConn(parent context.Context, conn net.Conn, timeout <-chan time.Time, busy bool) {
	defer func() { _ = conn.Close() }()
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	done := make(chan struct{})
	watcherDone := make(chan struct{})
	beginWrite := make(chan bool)
	permitted := make(chan bool)
	go func() {
		defer close(watcherDone)
		watchBoxConn(parent, conn, cancel, timeout, done, beginWrite, permitted)
	}()
	defer func() {
		close(done)
		<-watcherDone
	}()

	answer := boxmcp.Answer{Refusal: CannotReach}
	var forwarded bool
	var err error
	if !busy && ctx.Err() == nil {
		answer, forwarded, err = r.boxAnswer(ctx, conn)
	}
	// WriteAnswer validates before its first write. Treat a timeout during
	// that validation like any other timeout before the first answer byte.
	interrupted := errors.New("box answer interrupted before writing")
	var writing bool
	write := func(forced bool) boxWriter {
		return func(p []byte) (int, error) {
			if !writing {
				beginWrite <- forced
				if !<-permitted {
					return 0, interrupted
				}
				writing = true
			}
			return conn.Write(p)
		}
	}

	if writeErr := boxmcp.WriteAnswer(write(false), answer); errors.Is(writeErr, boxmcp.ErrAnswer) || errors.Is(writeErr, interrupted) {
		_ = boxmcp.WriteAnswer(write(true), boxmcp.Answer{Refusal: CannotReach})
		err = writeErr
		if ctx.Err() != nil {
			err = ctx.Err()
		}
	}
	if forwarded && err != nil {
		r.diagnose(DiagForward, err)
	}
}

// A timeout before writing still permits the refusal. Give that short write a
// bounded grace period when the peer stops reading.
const refusalWriteGrace = 250 * time.Millisecond

func watchBoxConn(parent context.Context, conn net.Conn, cancel context.CancelFunc,
	timeout <-chan time.Time, done <-chan struct{}, beginWrite <-chan bool, permitted chan<- bool,
) {
	parentDone := parent.Done()
	var writing, expired bool
	interrupt := func() {
		expired = true
		cancel()
		if writing {
			_ = conn.Close()
		} else {
			_ = conn.SetReadDeadline(time.Now())
		}
	}
	for {
		select {
		case <-done:
			return
		case <-timeout:
			timeout = nil
			interrupt()
		case <-parentDone:
			parentDone = nil
			interrupt()
		case forced := <-beginWrite:
			// A single watcher consumes the timer and grants the first
			// write. Check an expiration already ready before granting.
			select {
			case <-timeout:
				timeout = nil
				interrupt()
			default:
			}
			if parentDone != nil && parent.Err() != nil {
				parentDone = nil
				interrupt()
			}
			if expired {
				if !forced {
					permitted <- false
					continue
				}
				_ = conn.SetWriteDeadline(time.Now().Add(refusalWriteGrace))
			}
			writing = true
			permitted <- true
		}
	}
}

type boxWriter func([]byte) (int, error)

func (w boxWriter) Write(p []byte) (int, error) { return w(p) }

func (r *reconciler) boxAnswer(ctx context.Context, conn net.Conn) (boxmcp.Answer, bool, error) {
	reader := bufio.NewReaderSize(io.LimitReader(conn, protocol.MaxBoxRequest+1), protocol.MaxBoxRequest+1)
	line, err := reader.ReadSlice('\n')
	if err != nil || r.latest.Load() == nil {
		return boxmcp.Answer{Refusal: CannotReach}, false, nil
	}
	uid, err := r.cfg.PeerUID(conn)
	if err != nil || !activeUser(r.latest.Load(), uid) {
		return boxmcp.Answer{Refusal: NotActiveUser}, false, nil
	}
	line = line[:len(line)-1]
	if protocol.BoxMethod(line) == "" {
		return boxmcp.Answer{Refusal: CannotReach}, false, nil
	}
	answer, err := r.forward(ctx, uid, line)
	if err != nil {
		answer = boxmcp.Answer{Refusal: CannotReach}
	}
	return answer, true, err
}

func activeUser(state *protocol.DesiredState, uid int64) bool {
	if uid == 0 {
		return false
	}
	for _, user := range state.Users {
		if user.UID != uid || user.State != protocol.UserActive {
			continue
		}
		for _, account := range state.Accounts {
			if account.Slug == user.Account && account.State == protocol.AccountActive {
				return true
			}
		}
	}
	return false
}

func (r *reconciler) forward(ctx context.Context, uid int64, line []byte) (boxmcp.Answer, error) {
	path := protocol.BoxPath + "?" + protocol.QueryUID + "=" + strconv.FormatInt(uid, 10)
	client := *r.cfg.HTTP
	client.CheckRedirect = noRedirect
	resp, err := r.request(ctx, &client, http.MethodPost, path, bytes.NewReader(line), true)
	if err != nil {
		return boxmcp.Answer{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusForbidden {
		return boxmcp.Answer{Refusal: NotActiveUser}, nil
	}
	if err := checkStatus(resp, http.StatusOK); err != nil {
		return boxmcp.Answer{}, err
	}
	buffer := make([]byte, protocol.MaxBoxAnswer+1)
	var size int
	for size < len(buffer) {
		n, err := resp.Body.Read(buffer[size:])
		size += n
		if size > protocol.MaxBoxAnswer {
			break
		}
		if errors.Is(err, io.EOF) {
			return boxmcp.Answer{Message: bytes.TrimRight(buffer[:size], "\n")}, nil
		}
		if err != nil {
			return boxmcp.Answer{}, err
		}
	}
	return boxmcp.Answer{}, protocol.ErrTooLarge
}
