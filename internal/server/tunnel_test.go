package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestTunnelAcceptsFirstFrameBearer(t *testing.T) {
	jwtSecret := "jwt-secret"
	s := &Server{auth: NewAuth(jwtSecret)}
	server := httptest.NewServer(http.HandlerFunc(s.handleTunnel))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(TunnelRequest{Authorization: "Bearer " + signedAdminToken(t, jwtSecret)}); err != nil {
		t.Fatalf("write init frame: %v", err)
	}

	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read response frame: %v", err)
	}
	if string(msg) != `{"error":"url required"}` {
		t.Fatalf("expected auth to pass and url validation to run, got %s", msg)
	}
}

func TestTunnelRejectsMissingFirstFrameAuth(t *testing.T) {
	s := &Server{auth: NewAuth("jwt-secret")}
	server := httptest.NewServer(http.HandlerFunc(s.handleTunnel))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(TunnelRequest{URL: "wss://target.example/ws"}); err != nil {
		t.Fatalf("write init frame: %v", err)
	}

	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read response frame: %v", err)
	}
	if string(msg) != `{"error":"unauthorized"}` {
		t.Fatalf("expected unauthorized response, got %s", msg)
	}
}

func TestTunnelWebSocketTargetUsesHeadersPayloadAndRelaysFrames(t *testing.T) {
	upstreamUpgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Tunnel-Test"); got != "yes" {
			http.Error(w, "missing tunnel header", http.StatusBadRequest)
			return
		}
		conn, err := upstreamUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		messageType, msg, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("read upstream payload: %v", err)
			return
		}
		if string(msg) != "hello upstream" {
			t.Errorf("payload = %q, want hello upstream", msg)
			return
		}
		_ = conn.SetReadDeadline(time.Time{})
		if err := conn.WriteMessage(messageType, []byte("payload:"+string(msg))); err != nil {
			return
		}
		for {
			messageType, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(messageType, []byte("echo:"+string(msg))); err != nil {
				return
			}
		}
	}))
	defer upstream.Close()

	jwtSecret := "jwt-secret"
	s := &Server{auth: NewAuth(jwtSecret), allowPrivateTargets: true}
	gateway := httptest.NewServer(http.HandlerFunc(s.handleTunnel))
	defer gateway.Close()

	wsURL := "ws" + strings.TrimPrefix(gateway.URL, "http")
	targetURL := "ws" + strings.TrimPrefix(upstream.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial gateway websocket: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(TunnelRequest{
		URL:           targetURL,
		Authorization: "Bearer " + signedAdminToken(t, jwtSecret),
		Headers:       map[string]string{"X-Tunnel-Test": "yes"},
		Payload:       "hello upstream",
	}); err != nil {
		t.Fatalf("write init frame: %v", err)
	}

	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read connected frame: %v", err)
	}
	if string(msg) != `{"status":"connected"}` {
		t.Fatalf("expected connected response, got %s", msg)
	}

	_, msg, err = conn.ReadMessage()
	if err != nil {
		t.Fatalf("read payload relay: %v", err)
	}
	if string(msg) != "payload:hello upstream" {
		t.Fatalf("expected payload relay, got %s", msg)
	}

	if err := conn.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
		t.Fatalf("write tunnel frame: %v", err)
	}
	_, msg, err = conn.ReadMessage()
	if err != nil {
		t.Fatalf("read echo frame: %v", err)
	}
	if string(msg) != "echo:ping" {
		t.Fatalf("expected echo relay, got %s", msg)
	}
}
