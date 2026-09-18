// Copyright (c) 2026 Canonical Ltd
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License version 3 as
// published by the Free Software Foundation.

package cmdstate

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/websocket"
)

func TestConnectRejectsDuplicateAfterUpgrade(t *testing.T) {
	e := &execution{
		websockets:       map[string]*websocket.Conn{wsControl: nil},
		controlConnected: make(chan struct{}),
	}
	errors := make(chan error, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		errors <- e.connect(r, w, wsControl)
	}))
	defer server.Close()

	wsURL := "ws" + server.URL[len("http"):] + "/"
	first, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("cannot dial first websocket: %v", err)
	}
	defer first.Close()
	if err := <-errors; err != nil {
		t.Fatalf("cannot connect first websocket: %v", err)
	}
	connected := e.websockets[wsControl]

	second, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		second.Close()
		t.Fatal("duplicate websocket unexpectedly connected")
	}
	if err := <-errors; err == nil || err.Error() != "control websocket already connected" {
		t.Fatalf("unexpected duplicate connection error: %v", err)
	}
	if e.websockets[wsControl] != connected {
		t.Fatal("duplicate websocket replaced the connected websocket")
	}
}
