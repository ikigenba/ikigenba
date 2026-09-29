package main

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
	"unsafe"
)

const leaveScreen = "\x1b[?25h\x1b[?1049l"

type machineConsole struct {
	mu       sync.Mutex
	open     bool
	saved    *syscall.Termios
	keys     chan []byte
	resized  chan struct{}
	winch    chan os.Signal
	done     chan struct{}
	signals  *machineSignals
	readOnce sync.Once
}

func newMachineConsole() *machineConsole {
	return &machineConsole{keys: make(chan []byte), resized: make(chan struct{}, 1), winch: make(chan os.Signal, 1), done: make(chan struct{})}
}

func setTermios(state *syscall.Termios) syscall.Errno {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, os.Stdin.Fd(), uintptr(syscall.TCSETS), uintptr(unsafe.Pointer(state)))
	return errno
}

func (c *machineConsole) Raw() func() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.open = true
	c.signals.startRaw()
	var saved syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, os.Stdin.Fd(), uintptr(syscall.TCGETS), uintptr(unsafe.Pointer(&saved)))
	if errno == 0 {
		raw := saved
		raw.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP | syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
		raw.Oflag &^= syscall.OPOST
		raw.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
		raw.Cflag &^= syscall.CSIZE | syscall.PARENB
		raw.Cflag |= syscall.CS8
		raw.Cc[syscall.VMIN] = 1
		raw.Cc[syscall.VTIME] = 0
		if setTermios(&raw) == 0 {
			c.saved = &saved
		}
	}
	signal.Notify(c.winch, syscall.SIGWINCH)
	c.readOnce.Do(func() {
		go c.readKeys()
		go c.readResizes()
	})
	return func() { c.restore(false) }
}

func (c *machineConsole) restore(leave bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.open {
		return
	}
	if leave {
		_, _ = os.Stdout.WriteString(leaveScreen)
	}
	if c.saved != nil {
		_ = setTermios(c.saved)
	}
	c.open = false
	close(c.done)
	signal.Stop(c.winch)
	c.signals.stopRaw()
}

func (c *machineConsole) cleanup()                 { c.restore(true) }
func (c *machineConsole) Keys() <-chan []byte      { return c.keys }
func (c *machineConsole) Resized() <-chan struct{} { return c.resized }

func (*machineConsole) Size() (cols, rows int) {
	var size struct{ rows, cols, xpixel, ypixel uint16 }
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, os.Stdout.Fd(), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&size)))
	if errno != 0 {
		return 0, 0
	}
	return int(size.cols), int(size.rows)
}

func (c *machineConsole) readKeys() {
	defer close(c.keys)
	var buffer [4096]byte
	for {
		n, err := os.Stdin.Read(buffer[:])
		if n > 0 {
			key := append([]byte(nil), buffer[:n]...)
			select {
			case c.keys <- key:
			case <-c.done:
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func (c *machineConsole) readResizes() {
	for {
		select {
		case <-c.done:
			return
		case <-c.winch:
			select {
			case c.resized <- struct{}{}:
			default:
			}
		}
	}
}
