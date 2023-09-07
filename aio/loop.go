package aio

import (
	"context"
	"log/slog"
	"math"
	"os"
	"syscall"
	"time"
	"unsafe"

	"github.com/pawelgaczynski/giouring"
	"golang.org/x/sys/unix"
)

const (
	batchSize      = 128
	buffersGroupID = 0 // currently using only 1 provided buffer group
)

type completionCallback = func(res int32, flags uint32, errno syscall.Errno)
type operation = func(*giouring.SubmissionQueueEntry)

type Loop struct {
	ring      *giouring.Ring
	callbacks callbacks
	buffers   providedBuffers
	pending   []operation
}

type Options struct {
	RingEntries      uint32
	RecvBuffersCount uint32
	RecvBufferLen    uint32
}

var DefaultOptions = Options{
	RingEntries:      1024,
	RecvBuffersCount: 256,
	RecvBufferLen:    4 * 1024,
}

func New(opt Options) (*Loop, error) {
	ring, err := giouring.CreateRing(opt.RingEntries)
	if err != nil {
		return nil, err
	}
	l := &Loop{ring: ring}
	l.callbacks.init()
	if err := l.buffers.setup(ring, opt.RecvBuffersCount, opt.RecvBufferLen); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *Loop) RunOnce() error {
	if err := l.submitAndWait(1); err != nil {
		return err
	}
	_ = l.flushCompletions()
	return nil
}

// Run until all prepared operations are finished.
func (l *Loop) RunUntilDone() error {
	for {
		if l.callbacks.count() == 0 {
			return nil
		}
		if err := l.RunOnce(); err != nil {
			return err
		}
	}
}

func (l *Loop) Run(ctx context.Context, onCtxDone func()) error {
	if err := l.RunCtx(ctx, time.Millisecond*333); err != nil {
		return err
	}
	// call handler to
	onCtxDone()
	// run loop until all operations finishes
	if err := l.RunUntilDone(); err != nil {
		return err
	}
	return nil
}

// RunCtx until context is canceled.
// Check context every timeout.
func (l *Loop) RunCtx(ctx context.Context, timeout time.Duration) error {
	ts := syscall.NsecToTimespec(int64(timeout))
	done := func() bool {
		select {
		case <-ctx.Done():
			return true
		default:
		}
		return false
	}
	for {
		if err := l.submit(); err != nil {
			return err
		}
		if _, err := l.ring.WaitCQEs(1, &ts, nil); err != nil && !TemporaryErr(err) {
			return err
		}
		_ = l.flushCompletions()
		if done() {
			break
		}
	}
	return nil
}

// is this error temporary
func TemporaryErr(err error) bool {
	if errno, ok := err.(syscall.Errno); ok {
		return TemporaryErrno(errno)
	}
	if os.IsTimeout(err) {
		return true
	}
	return false
}

func TemporaryErrno(errno syscall.Errno) bool {
	return errno.Temporary() || errno == unix.ETIME || errno == syscall.ENOBUFS
}

// Retries on temporary errors.
// Anything not handled here is fatal and application should terminate.
// Errors that can be returned by [io_uring_enter].
//
// [io_uring_enter]: https://manpages.debian.org/unstable/liburing-dev/io_uring_enter.2.en.html#ERRORS
func (l *Loop) submitAndWait(waitNr uint32) error {
	for {
		if len(l.pending) > 0 {
			_, err := l.ring.SubmitAndWait(0)
			if err == nil {
				l.preparePending()
			}
		}

		_, err := l.ring.SubmitAndWait(waitNr)
		if err != nil && TemporaryErr(err) {
			continue
		}
		return err
	}
}

func (l *Loop) preparePending() {
	prepared := 0
	for _, op := range l.pending {
		sqe := l.ring.GetSQE()
		if sqe == nil {
			break
		}
		op(sqe)
		prepared++
	}
	if prepared == len(l.pending) {
		l.pending = nil
	} else {
		l.pending = l.pending[prepared:]
	}
}

func (l *Loop) submit() error {
	return l.submitAndWait(0)
}

func (l *Loop) flushCompletions() uint32 {
	var cqes [batchSize]*giouring.CompletionQueueEvent
	var noCompleted uint32 = 0
	for {
		peeked := l.ring.PeekBatchCQE(cqes[:])
		for _, cqe := range cqes[:peeked] {
			errno := cqeErrno(cqe)
			if cqe.UserData == 0 {
				slog.Debug("ceq without userdata", "res", cqe.Res, "flags", cqe.Flags, "errno", errno)
				continue
			}
			cb := l.callbacks.get(cqe)
			cb(cqe.Res, cqe.Flags, errno)

		}
		l.ring.CQAdvance(peeked)
		noCompleted += peeked
		if peeked < uint32(len(cqes)) {
			return noCompleted
		}
	}
}

func (l *Loop) getSQE() (*giouring.SubmissionQueueEntry, error) {
	for {
		sqe := l.ring.GetSQE()
		if sqe == nil {
			if err := l.submit(); err != nil {
				return nil, err
			}
			continue
		}
		return sqe, nil
	}
}

func (l *Loop) Close() {
	l.ring.QueueExit()
}

// prepares operation or adds it to pending if can't get sqe
func (l *Loop) prepare(op operation) {
	sqe := l.ring.GetSQE()
	if sqe == nil { // submit and retry
		l.submit()
		sqe = l.ring.GetSQE()
	}
	if sqe == nil { // still nothing, add to pending
		l.pending = append(l.pending, op)
		return
	}
	op(sqe)
}

func (l *Loop) PrepareMultishotAccept(fd int, cb completionCallback) {
	l.prepare(func(sqe *giouring.SubmissionQueueEntry) {
		sqe.PrepareMultishotAccept(fd, 0, 0, 0)
		l.callbacks.set(sqe, cb)
	})
}

func (l *Loop) PrepareCancelFd(fd int, cb completionCallback) {
	l.prepare(func(sqe *giouring.SubmissionQueueEntry) {
		sqe.PrepareCancelFd(fd, 0)
		l.callbacks.set(sqe, cb)
	})
}

func (l *Loop) PrepareShutdown(fd int, cb completionCallback) {
	l.prepare(func(sqe *giouring.SubmissionQueueEntry) {
		const SHUT_RDWR = 2
		sqe.PrepareShutdown(fd, SHUT_RDWR)
		l.callbacks.set(sqe, cb)
	})
}

func (l *Loop) PrepareClose(fd int, cb completionCallback) {
	l.prepare(func(sqe *giouring.SubmissionQueueEntry) {
		sqe.PrepareClose(fd)
		l.callbacks.set(sqe, cb)
	})
}

func (l *Loop) PrepareSend(fd int, buf []byte, cb completionCallback) {
	l.prepare(func(sqe *giouring.SubmissionQueueEntry) {
		sqe.PrepareSend(fd, uintptr(unsafe.Pointer(&buf[0])), uint32(len(buf)), 0)
		l.callbacks.set(sqe, cb)
	})
}

// references from std lib:
// https://github.com/golang/go/blob/140266fe7521bf75bf0037f12265190213cc8e7d/src/internal/poll/writev.go#L16
// https://github.com/golang/go/blob/140266fe7521bf75bf0037f12265190213cc8e7d/src/internal/poll/fd_writev_unix.go#L20
func (l *Loop) PrepareWritev(fd int, buffers [][]byte, cb completionCallback) {
	l.prepare(func(sqe *giouring.SubmissionQueueEntry) {
		var iovecs []syscall.Iovec
		for _, buf := range buffers {
			if len(buf) == 0 {
				continue
			}
			iovecs = append(iovecs, syscall.Iovec{Base: &buf[0]})
			iovecs[len(iovecs)-1].SetLen(len(buf))
		}
		sqe.PrepareWritev(fd, uintptr(unsafe.Pointer(&iovecs[0])), uint32(len(iovecs)), 0)
		l.callbacks.set(sqe, cb)
	})
}

// Multishot, provided buffers recv
func (l *Loop) PrepareRecv(fd int, cb completionCallback) {
	l.prepare(func(sqe *giouring.SubmissionQueueEntry) {
		sqe.PrepareRecvMultishot(fd, 0, 0, 0)
		sqe.Flags = giouring.SqeBufferSelect
		sqe.BufIG = buffersGroupID
		l.callbacks.set(sqe, cb)
	})
}

func (l *Loop) PrepareConnect(fd int, so syscall.Sockaddr, cb completionCallback) {
	l.prepare(func(sqe *giouring.SubmissionQueueEntry) {
		if err := sqe.PrepareConnect(fd, so); err != nil {
			panic(err) // only if tcp port is out of range
		}
		l.callbacks.set(sqe, cb)
	})
}

func cqeErrno(c *giouring.CompletionQueueEvent) syscall.Errno {
	if c.Res > -4096 && c.Res < 0 {
		return syscall.Errno(-c.Res)
	}
	return 0
}

// #region providedBuffers

type providedBuffers struct {
	br      *giouring.BufAndRing
	data    []byte
	entries uint32
	bufLen  uint32
}

func (b *providedBuffers) setup(ring *giouring.Ring, entries uint32, bufLen uint32) error {
	b.entries = entries
	b.bufLen = bufLen
	b.data = make([]byte, b.entries*b.bufLen)
	var err error
	b.br, err = ring.SetupBufRing(b.entries, buffersGroupID, 0)
	if err != nil {
		return err
	}
	for i := uint32(0); i < b.entries; i++ {
		b.br.BufRingAdd(
			uintptr(unsafe.Pointer(&b.data[b.bufLen*i])),
			b.bufLen,
			uint16(i),
			giouring.BufRingMask(b.entries),
			int(i),
		)
	}
	b.br.BufRingAdvance(int(b.entries))
	return nil
}

// get provided buffer from cqe res, flags
func (b *providedBuffers) get(res int32, flags uint32) ([]byte, uint16) {
	isProvidedBuffer := flags&giouring.CQEFBuffer > 0
	if !isProvidedBuffer {
		panic("missing buffer flag")
	}
	bufferID := uint16(flags >> giouring.CQEBufferShift)
	start := uint32(bufferID) * b.bufLen
	n := uint32(res)
	return b.data[start : start+n], bufferID
}

// return provided buffer to the kernel
func (b *providedBuffers) release(buf []byte, bufferID uint16) {
	b.br.BufRingAdd(
		uintptr(unsafe.Pointer(&buf[0])),
		b.bufLen,
		uint16(bufferID),
		giouring.BufRingMask(b.entries),
		0,
	)
	b.br.BufRingAdvance(1)
}

//#endregion providedBuffers

// #region callbacks

type callbacks struct {
	m    map[uint64]completionCallback
	next uint64
}

func (c *callbacks) init() {
	c.m = make(map[uint64]completionCallback)
	c.next = math.MaxUint16 // reserve first few userdata values for internal use
}

func (c *callbacks) set(sqe *giouring.SubmissionQueueEntry, cb completionCallback) {
	c.next++
	key := c.next
	c.m[key] = cb
	sqe.UserData = key
}

func (c *callbacks) get(cqe *giouring.CompletionQueueEvent) completionCallback {
	ms := isMultiShot(cqe.Flags)
	cb := c.m[cqe.UserData]
	if !ms {
		delete(c.m, cqe.UserData)
	}
	return cb
}

func (c *callbacks) count() int {
	return len(c.m)
}

// #endregion

func isMultiShot(flags uint32) bool {
	return flags&giouring.CQEFMore > 0
}
