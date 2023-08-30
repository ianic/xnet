package main

import (
	"fmt"
	"log"
	"log/slog"

	"github.com/ianic/xnet/aio"
	"github.com/ianic/xnet/aio/signal"
	"github.com/ianic/xnet/ws"
)

func main() {
	if err := run("127.0.0.1:4243"); err != nil {
		log.Panic(err)
	}
}

func run(ipPort string) error {
	// create loop
	loop, err := aio.New(aio.DefaultOptions)
	if err != nil {
		return err
	}
	defer loop.Close()

	chat := newChat()

	// start tcp listener
	lsn, err := aio.NewTcpListener(loop, ipPort,
		func(fd int, tc *aio.TcpConn, bind func(aio.Upstream)) {
			// start handshake for accepted connection
			bind(&handshake{
				tcpConn: tc,
				upgrade: func(wc *ws.AsyncConn) {
					wc.Bind(chat.newClient(fd, wc)) // bind websocket to upstream client
					bind(wc)                        // bind tcp conn to websocket conn
				},
			})
		})
	if err != nil {
		return err
	}
	if err := loop.Run(signal.InterruptContext(), func() { lsn.Close() }); err != nil {
		slog.Error("run", "error", err)
	}

	return nil
}

type handshake struct {
	tcpConn *aio.TcpConn
	upgrade func(*ws.AsyncConn)
}

func (h *handshake) Received(data []byte) {
	hs, err := ws.NewHandshakeFromBuffer(data)
	if err != nil {
		slog.Info("handshake failed", slog.String("error", err.Error()))
		h.upgrade(nil)
		h.tcpConn.Close()
		return
	}
	h.upgrade(hs.NewAsyncConn(h.tcpConn))
	h.tcpConn.Send([]byte(hs.Response()))
}
func (h *handshake) Closed(error) {}
func (h *handshake) Sent()        {}

type conn interface {
	Send([]byte)
	Close()
}

type client struct {
	fd         int
	conn       conn
	chat       *chat
	pos        int
	sendActive bool
}

func (c *client) Received(data []byte) {
	c.chat.post(data)
	fmt.Printf("%s", data)
}
func (c *client) Closed(err error) {
	c.chat.remove(c.fd)
	fmt.Printf("removed %d, close reason %s\n", c.fd, err)
}
func (c *client) Sent() {
	c.pos++
	c.sendActive = false
	c.send()
}

func (c *client) send() {
	if c.sendActive {
		return
	}
	if c.pos < len(c.chat.posts) {
		c.sendActive = true
		c.conn.Send(c.chat.posts[c.pos])

	}
}

type chat struct {
	posts   [][]byte
	clients map[int]*client
}

func newChat() *chat {
	return &chat{
		clients: make(map[int]*client),
	}
}

func (c *chat) remove(fd int) {
	delete(c.clients, fd)
}

func (c *chat) post(data []byte) {
	c.posts = append(c.posts, data)
	for _, cli := range c.clients {
		cli.send()
	}
}

func (c *chat) newClient(fd int, conn conn) *client {
	cli := &client{fd: fd, conn: conn, chat: c, pos: -1}
	if _, ok := c.clients[fd]; ok {
		panic("client fd used")
	}
	c.clients[fd] = cli
	return cli
}
