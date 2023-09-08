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

	// called when ws handshake is successful
	// upgrades tcp connection to websocket connection
	wsAccepted := func(fd int, tc *aio.TCPConn, wc *ws.AsyncConn) {
		cli := chat.newClient(fd, wc)
		tc.Bind(wc)  // rebind tcp connection from handshake to websocket
		wc.Bind(cli) // bind websocket to upstream chat client
	}
	// called when tcp listener accepts tcp connection
	tcpAccepted := func(fd int, tc *aio.TCPConn) {
		tc.Bind(&handshake{
			fd:         fd,
			tcpConn:    tc,
			wsAccepted: wsAccepted,
		})
	}

	// start tcp listener
	_, err = loop.Listen(ipPort, tcpAccepted)
	if err != nil {
		return err
	}
	// run loop, this is blocking
	if err := loop.Run(signal.InterruptContext()); err != nil {
		slog.Error("run", "error", err)
	}

	return nil
}

type handshake struct {
	fd         int
	tcpConn    *aio.TCPConn
	wsAccepted func(int, *aio.TCPConn, *ws.AsyncConn)
}

func (h *handshake) Received(data []byte) {
	hs, err := ws.NewHandshakeFromBuffer(data)
	if err != nil {
		slog.Info("handshake failed", slog.String("error", err.Error()))
		//h.upgrade(nil)
		h.tcpConn.Close()
		return
	}
	h.wsAccepted(h.fd, h.tcpConn, hs.NewAsyncConn(h.tcpConn))
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
