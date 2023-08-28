package ws

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

type Handshake struct {
	version   string
	key       string
	host      string
	extension Extension
}

type Extension struct {
	permessageDeflate       bool
	serverNoContextTakeover bool
	clientNoContextTakeover bool
	serverMaxWindowBits     int // -1 not set, 0 param without value, >0 value set by client
	clientMaxWindowBits     int
}

const (
	crlf       = "\r\n"
	requestEnd = crlf + crlf
)

func (hs *Handshake) Response() string {
	var b strings.Builder

	lines := []string{
		"HTTP/1.1 101 Switching Protocols",
		"Upgrade: websocket",
		"Connection: Upgrade",
		"Sec-WebSocket-Accept: %s",
	}
	extension := "Sec-WebSocket-Extensions: permessage-deflate; client_no_context_takeover; server_no_context_takeover"

	for _, line := range lines {
		b.WriteString(line)
		b.WriteString(crlf)
	}
	if hs.extension.permessageDeflate {
		b.WriteString(extension)
		b.WriteString(crlf)
	}
	b.WriteString(crlf)
	return fmt.Sprintf(b.String(), secAccept(hs.key))
}

func NewHandshake(reader *bufio.Reader) (Handshake, error) {
	req, err := http.ReadRequest(reader)
	if err != nil {
		return Handshake{}, err
	}
	return NewHandshakeFromRequest(req)
}

func NewHandshakeFromRequest(req *http.Request) (Handshake, error) {
	hs := Handshake{
		host: req.Host,
	}
	// set to unseen values
	hs.extension.clientMaxWindowBits = -1
	hs.extension.serverMaxWindowBits = -1

	upgradeHeaders := 0

	for key, value := range req.Header {
		if len(value) == 0 {
			continue
		}
		val := value[0]
		switch strings.ToLower(key) {
		case "sec-websocket-key":
			hs.key = val
		case "sec-websocket-version":
			hs.version = val
		case "connection":
			if strings.ToLower(val) == "upgrade" {
				upgradeHeaders += 1
			}
		case "upgrade":
			if strings.ToLower(val) == "websocket" {
				upgradeHeaders += 1
			}
		case "sec-websocket-extensions":

			hs.extension.permessageDeflate = strings.Contains(val, "permessage-deflate")
			hs.extension.serverNoContextTakeover = strings.Contains(val, "server_no_context_takeover")
			hs.extension.clientNoContextTakeover = strings.Contains(val, "client_no_context_takeover")
			if strings.Contains(val, "server_max_window_bits") {
				hs.extension.serverMaxWindowBits = 0
			}
			if strings.Contains(val, "client_max_window_bits") {
				hs.extension.clientMaxWindowBits = 0
			}

			for _, part := range strings.Split(val, ";") {
				kv := strings.Split(part, "=")
				if len(kv) != 2 {
					continue
				}
				k := strings.TrimSpace(kv[0])
				v := strings.TrimSpace(kv[1])

				i, err := strconv.Atoi(v)
				if err != nil {
					continue
				}

				switch k {
				case "server_max_window_bits":
					hs.extension.serverMaxWindowBits = i
				case "client_max_window_bits":
					hs.extension.clientMaxWindowBits = i
				}
			}

		}
	}
	if upgradeHeaders != 2 {
		return Handshake{}, errors.New("upgrade headers not found")
	}

	return hs, nil
}

// Generate random sec key.
// Used on client to send Sec-WebSocket-Key header
func secKey() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf), nil
}

// Generate Sec-WebSocket-Accept key header value on server from clients key.
func secAccept(key string) string {
	const WsMagicKey = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

	h := sha1.New()
	io.WriteString(h, key)
	io.WriteString(h, WsMagicKey)
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
