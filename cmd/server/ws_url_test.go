package main

import "testing"

func TestNormalizeWebsocketURL(t *testing.T) {
	cases := map[string]string{
		"https://meet.sc.tt":            "wss://meet.sc.tt/websocket",
		"https://meet.sc.tt/":           "wss://meet.sc.tt/websocket",
		"http://localhost:3000":         "ws://localhost:3000/websocket",
		"wss://x.example.com/websocket": "wss://x.example.com/websocket",
		"https://app.example.com/ws":    "wss://app.example.com/ws",
		"":                              "",
	}
	for in, want := range cases {
		if got := normalizeWebsocketURL(in); got != want {
			t.Errorf("normalizeWebsocketURL(%q)=%q want %q", in, got, want)
		}
	}
}
