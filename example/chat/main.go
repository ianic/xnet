package main

import (
	"log"
	"net/http"

	"github.com/ianic/ws"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	address := "localhost:3002"
	log.Printf("starting websocket server at %s", address)

	input := make(chan ClientMsg)
	output := make(chan ServerMsg)

	server := Server{
		input:    input,
		output:   output,
		connects: make(chan *ws.Conn),
	}
	chat := Chat{
		input:  input,
		output: output,
	}
	go chat.loop()
	go server.loop()

	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		wc, err := ws.NewFromRequest(w, r)
		if err != nil {
			log.Printf("ws connect error %s", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		// log.Printf("ws connect OK")
		server.Connect(wc)
		// Echo(wc)
	})
	err := http.ListenAndServe(address, nil)
	if err != nil {
		log.Fatal(err)
	}
}

type ClientMsgType byte

const (
	clientPost ClientMsgType = iota
	clientConnect
	clientDisconnect
	clientAck
)

type ClientMsg struct {
	ClientID int
	Post     string
	Ack      int
	Type     ClientMsgType
}

type ServerMsg struct {
	ClientID int
	Ack      int
	Posts    []string
}

type Server struct {
	input    chan ClientMsg
	output   chan ServerMsg
	connects chan *ws.Conn
}

func (s *Server) loop() {
	conns := make(map[int]*ws.Conn)
	nextID := 0
	for {
		select {
		case m := <-s.output:
			wc, ok := conns[m.ClientID]
			if !ok {
				log.Printf("connection not found %d", m.ClientID)
				break
			}
			go func(m ServerMsg) {
				if m.Posts != nil {
					for _, t := range m.Posts {
						if err := wc.WriteText([]byte(t)); err != nil {
							return
						}
					}
				}
				s.input <- ClientMsg{
					ClientID: m.ClientID,
					Ack:      m.Ack,
					Type:     clientAck,
				}
			}(m)
		case wc := <-s.connects:
			nextID++
			clientID := nextID
			conns[clientID] = wc
			go s.readLoop(clientID, wc)
		}
	}
}

func (s *Server) Connect(wc *ws.Conn) {
	s.connects <- wc
}

func (s *Server) readLoop(clientID int, wc *ws.Conn) {
	log.Printf("connect %d", clientID)
	s.input <- ClientMsg{ClientID: clientID, Type: clientConnect}
	for {
		opcode, payload, err := wc.Read()
		if err != nil {
			break
		}
		if opcode != ws.Text {
			log.Printf("unhandled opcode %d", opcode)
			continue
		}
		s.input <- ClientMsg{ClientID: clientID, Type: clientPost, Post: string(payload)}
	}
	s.input <- ClientMsg{ClientID: clientID, Type: clientDisconnect}
	log.Printf("disconnect %d", clientID)
}

type Chat struct {
	input  chan ClientMsg
	output chan ServerMsg
}

type Client struct {
	pos    int
	active bool
}

func (chat *Chat) loop() {
	clients := make(map[int]*Client)
	var state []string

	for m := range chat.input {
		switch m.Type {
		case clientConnect:
			client := &Client{
				pos:    0,
				active: true,
			}
			clients[m.ClientID] = client
			ack := len(state)
			if ack > 0 {
				client.active = true
				chat.output <- ServerMsg{
					ClientID: m.ClientID,
					Ack:      ack,
					Posts:    state[client.pos:ack],
				}
			}
		case clientDisconnect:
			delete(clients, m.ClientID)
		case clientAck:
			client, ok := clients[m.ClientID]
			if !ok {
				panic("client not found")
			}
			client.pos = m.Ack
			client.active = false
			ack := len(state)
			if len(state) > client.pos {
				client.active = true
				chat.output <- ServerMsg{
					ClientID: m.ClientID,
					Ack:      ack,
					Posts:    state[client.pos:ack],
				}
			}
		case clientPost:
			if len(m.Post) == 0 {
				continue
			}
			state = append(state, m.Post)
			for id, client := range clients {
				if client.active {
					continue
				}
				ack := len(state)
				client.active = true
				chat.output <- ServerMsg{
					ClientID: id,
					Ack:      ack,
					Posts:    state[client.pos:ack],
				}
			}
		}
	}
}
