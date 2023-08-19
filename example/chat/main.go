package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/ianic/ws"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	address := "localhost:3002"
	log.Printf("starting websocket server at %s", address)

	chat := NewChat()
	server := NewServer(chat)

	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		wc, err := ws.NewFromRequest(w, r)
		if err != nil {
			log.Printf("ws connect error %s", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		server.Connect(wc)
	})

	srv := &http.Server{
		Addr: address,
	}
	go func() {
		wait()
		httpShutdown(srv)
	}()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	server.Shutdown()
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

type Server struct {
	chat        *Chat
	connects    chan *ws.Conn
	disconnects chan int
	done        chan struct{}
	wg          sync.WaitGroup
}

func NewServer(chat *Chat) *Server {
	s := Server{
		chat:        chat,
		connects:    make(chan *ws.Conn),
		disconnects: make(chan int),
		done:        make(chan struct{}),
	}
	go s.loop()
	return &s
}

func (s *Server) loop() {
	conns := make(map[int]*ws.Conn)
	nextID := 0
	for {
		select {
		case m, ok := <-s.chat.Updates:
			if !ok {
				close(s.done)
				return
			}
			wc, ok := conns[m.ID]
			if !ok {
				log.Printf("connection not found %d", m.ID)
				break
			}
			go func(u Update) {
				s.wg.Add(1)
				defer s.wg.Done()

				for _, t := range u.Posts {
					if err := wc.WriteText([]byte(t)); err != nil {
						return
					}
				}
				s.chat.Ack(u.ID, u.Ack)
			}(m)
		case wc, ok := <-s.connects:
			if !ok {
				for _, wc := range conns {
					wc.Close()
				}
				s.connects = nil // prevent further calls
				break
			}
			nextID++
			id := nextID
			conns[id] = wc
			go s.readLoop(id, wc)
		case id := <-s.disconnects:
			delete(conns, id)
		}
	}
}

func (s *Server) Shutdown() {
	close(s.connects)
	s.wg.Wait()
	s.chat.Close()
	<-s.done
}

func (s *Server) Connect(wc *ws.Conn) {
	s.connects <- wc
}

func (s *Server) readLoop(id int, wc *ws.Conn) {
	s.wg.Add(1)
	defer s.wg.Done()

	s.chat.Connect(id)
	// log.Printf("connect %d", id)
	for {
		opcode, payload, err := wc.Read()
		if err != nil {
			break
		}
		if opcode != ws.Text {
			log.Printf("unhandled opcode %d", opcode)
			continue
		}
		s.chat.Post(id, string(payload))
	}
	s.chat.Disconnect(id)
	s.disconnects <- id
	// log.Printf("disconnect %d", id)
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
