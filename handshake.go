package main

import (
	"bufio"
	"bytes"
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
	extension struct {
		permessageDeflate       bool
		serverNoContextTakeover bool
		clientNoContextTakeover bool
		serverMaxWindowBits     struct {
			included bool
			value    int
		}
		clientMaxWindowBits struct {
			included bool
			value    int
		}
	}
	key  string
	host string
}

func (hs *Handshake) Response() string {
	const crlf = "\r\n"
	return fmt.Sprintf(
		"HTTP/1.1 101 Switching Protocols"+crlf+
			"Upgrade: websocket"+crlf+
			"Connection: Upgrade"+crlf+
			"Sec-WebSocket-Accept: %s"+crlf+crlf,
		secAccept(hs.key))
}

func Parse(buf []byte) (*Handshake, error) {
	reader := bufio.NewReader(bytes.NewReader(buf))
	req, err := http.ReadRequest(reader)
	if err != nil {
		return nil, err
	}

	hs := Handshake{
		host: req.Host,
	}
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
			hs.extension.serverMaxWindowBits.included = strings.Contains(val, "server_max_window_bits")
			hs.extension.clientMaxWindowBits.included = strings.Contains(val, "client_max_window_bits")

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
					hs.extension.serverMaxWindowBits.value = i
				case "client_max_window_bits":
					hs.extension.clientMaxWindowBits.value = i
				}
			}

		}
	}
	if upgradeHeaders != 2 {
		return nil, errors.New("upgrade headers not found")
	}

	return &hs, nil
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
