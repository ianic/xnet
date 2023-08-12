package main

import (
	"bufio"
	"bytes"
	"errors"
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
