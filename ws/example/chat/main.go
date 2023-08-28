package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/ianic/xnet/ws"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	address := "localhost:3000"
	log.Printf("starting websocket server at %s", address)

	chat := NewChat()
	upgrader := ws.NewUpgrader(chat)
	go func(updates chan Update) {
		for u := range updates {
			go func(u Update) {
				for _, p := range u.Posts {
					if err := upgrader.Send(u.ID, []byte(p)); err != nil {
						slog.Error("upgrader.Send", "error", err)
						return
					}
				}
				chat.Ack(u.ID, u.Ack)
			}(u)
		}
	}(chat.Updates)

	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)
	http.HandleFunc("/ws", upgrader.Upgrade)

	srv := &http.Server{
		Addr: address,
	}
	go func() {
		wait()
		if err := httpShutdown(srv); err != nil {
			slog.Error("http.Server shutdown", "error", err)
		}
	}()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := upgrader.Shutdown(ctx); err != nil {
		slog.Error("ws.Upgrader shutdown", "error", err)
	}
	chat.Close()
}

func wait() {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, os.Kill)
	<-quit
}

func httpShutdown(srv *http.Server) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

type Update struct {
	ID    int
	Ack   int
	Posts []string
}

type Chat struct {
	Updates chan Update

	connects    chan int
	disconnects chan int
	posts       chan post
	acks        chan ack

	clients map[int]*Client
	state   []string
}

func NewChat() *Chat {
	c := Chat{
		Updates:     make(chan Update),
		connects:    make(chan int),
		disconnects: make(chan int),
		acks:        make(chan ack),
		posts:       make(chan post),
		clients:     make(map[int]*Client),
	}
	go c.loop()
	return &c
}

func (c *Chat) OnConnect(id int)           { c.Connect(id) }
func (c *Chat) OnDisconnect(id int)        { c.Disconnect(id) }
func (c *Chat) OnData(id int, data []byte) { c.Post(id, string(data)) }

type Client struct {
	pos    int
	active bool
}

func (c *Chat) Connect(id int) {
	c.connects <- id
}

func (c *Chat) connect(id int) {
	client := &Client{
		pos:    0,
		active: false,
	}
	c.clients[id] = client
	c.update(id, client)
}

func (c *Chat) Disconnect(id int) {
	c.disconnects <- id
}

func (c *Chat) disconnect(id int) {
	delete(c.clients, id)
}

type post struct {
	id   int
	text string
}

func (c *Chat) Post(id int, text string) {
	if len(text) == 0 {
		return
	}
	c.posts <- post{id: id, text: text}
}

func (c *Chat) post(p post) {
	c.state = append(c.state, fmt.Sprintf("%d: %s", p.id, p.text))
	c.updateAll()
}

func (c *Chat) updateAll() {
	for id, client := range c.clients {
		c.update(id, client)
	}
}

func (c *Chat) update(id int, client *Client) {
	if client.active {
		return
	}
	pos := len(c.state)
	if client.pos >= pos {
		return
	}
	client.active = true
	c.Updates <- Update{
		ID:    id,
		Ack:   pos,
		Posts: c.state[client.pos:pos],
	}
}

func (c *Chat) Ack(id int, pos int) {
	c.acks <- ack{id: id, pos: pos}
}

type ack struct {
	id  int
	pos int
}

func (c *Chat) ack(a ack) {
	client, ok := c.clients[a.id]
	if !ok {
		panic("client not found")
	}
	client.pos = a.pos
	client.active = false
	c.update(a.id, client)
}

func (c *Chat) Close() {
	close(c.connects)
}

func (c *Chat) loop() {
	for {
		select {
		case clientID, ok := <-c.connects:
			if !ok {
				close(c.Updates)
				return
			}
			c.connect(clientID)
		case clientID := <-c.disconnects:
			c.disconnect(clientID)
		case a := <-c.acks:
			c.ack(a)
		case p := <-c.posts:
			c.post(p)
		}
	}
}
