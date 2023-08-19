package main

import (
	"fmt"
	"log"
	"net/http"

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
	err := http.ListenAndServe(address, nil)
	if err != nil {
		log.Fatal(err)
	}
}

type Server struct {
	chat     *Chat
	connects chan *ws.Conn
}

func NewServer(chat *Chat) *Server {
	s := Server{
		chat:     chat,
		connects: make(chan *ws.Conn),
	}
	go s.loop()
	return &s
}

func (s *Server) loop() {
	conns := make(map[int]*ws.Conn)
	nextID := 0
	for {
		select {
		case m := <-s.chat.Updates:
			wc, ok := conns[m.ID]
			if !ok {
				log.Printf("connection not found %d", m.ID)
				break
			}
			go func(u Update) {
				for _, t := range u.Posts {
					if err := wc.WriteText([]byte(t)); err != nil {
						return
					}
				}
				s.chat.Ack(u.ID, u.Ack)
			}(m)
		case wc := <-s.connects:
			nextID++
			id := nextID
			conns[id] = wc
			go s.readLoop(id, wc)
		}
	}
}

func (s *Server) Connect(wc *ws.Conn) {
	s.connects <- wc
}

func (s *Server) readLoop(id int, wc *ws.Conn) {
	s.chat.Connect(id)
	log.Printf("connect %d", id)
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
	log.Printf("disconnect %d", id)
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

func (c *Chat) loop() {
	for {
		select {
		case clientID := <-c.connects:
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
