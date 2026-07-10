package ha

import "testing"

func TestWebsocketURL(t *testing.T) {
	cases := map[string]string{
		"http://homeassistant.local:8123":  "ws://homeassistant.local:8123/api/websocket",
		"https://ha.example.com":           "wss://ha.example.com/api/websocket",
		"http://supervisor/core":           "ws://supervisor/core/api/websocket",
		"http://homeassistant.local:8123/": "ws://homeassistant.local:8123/api/websocket",
	}
	for in, want := range cases {
		got, err := websocketURL(in)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if got != want {
			t.Errorf("websocketURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWebsocketURLRejectsBadScheme(t *testing.T) {
	if _, err := websocketURL("ftp://nope"); err == nil {
		t.Fatal("expected error for unsupported scheme")
	}
}
