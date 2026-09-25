package desktop

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestViewerRejectsRequestsBeforeOpeningGuestStream(t *testing.T) {
	token := strings.Repeat("a", 64)
	var calls atomic.Int32
	viewer := Server{Host: "127.0.0.1:12345", Token: token, Dial: func(context.Context) (net.Conn, error) {
		calls.Add(1)
		return nil, fmt.Errorf("unexpected guest request")
	}}
	for _, test := range []struct {
		name, path, host, origin, site, authorization, protocols string
		cookie, websocket                                        bool
		want                                                     int
	}{
		{name: "public bootstrap", path: "/", want: 200},
		{name: "private image", path: "/vnc.html", want: 401},
		{name: "private health", path: "/health", want: 401},
		{name: "DNS rebinding", path: "/health", host: "attacker.test", authorization: "Bearer " + token, want: 403},
		{name: "cross origin", path: "/health", origin: "http://attacker.test", authorization: "Bearer " + token, want: 403},
		{name: "other local port", path: "/health", origin: "http://127.0.0.1:12346", authorization: "Bearer " + token, want: 403},
		{name: "cross site", path: "/health", site: "cross-site", authorization: "Bearer " + token, want: 403},
		{name: "wrong token", path: "/health", authorization: "Bearer wrong", want: 401},
		{name: "missing scheme", path: "/health", authorization: token, want: 401},
		{name: "cookie cannot authorize", path: "/health", cookie: true, want: 401},
		{name: "websocket origin required", path: "/websockify", protocols: "binary, cooper-auth-" + token, websocket: true, want: 403},
		{name: "websocket token required", path: "/websockify", origin: "http://127.0.0.1:12345", protocols: "binary", websocket: true, want: 401},
		{name: "websocket rejects wrong token", path: "/websockify", origin: "http://127.0.0.1:12345", protocols: "binary, cooper-auth-wrong", websocket: true, want: 401},
		{name: "websocket binary required", path: "/websockify", origin: "http://127.0.0.1:12345", protocols: "cooper-auth-" + token, websocket: true, want: 401},
		{name: "websocket cookie cannot authorize", path: "/websockify", origin: "http://127.0.0.1:12345", protocols: "binary", cookie: true, websocket: true, want: 401},
		{name: "authenticated health", path: "/health", authorization: "Bearer " + token, want: 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "http://"+viewer.Host+test.path, nil)
			if test.host != "" {
				request.Host = test.host
			}
			request.Header.Set("Origin", test.origin)
			request.Header.Set("Sec-Fetch-Site", test.site)
			request.Header.Set("Authorization", test.authorization)
			request.Header.Set("Sec-WebSocket-Protocol", test.protocols)
			if test.websocket {
				request.Header.Set("Upgrade", "websocket")
			}
			if test.cookie {
				request.AddCookie(&http.Cookie{Name: "cooper-desktop-" + token[:16], Value: token})
			}
			response := httptest.NewRecorder()
			viewer.Handler().ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d", response.Code, test.want)
			}
			if strings.Contains(response.Body.String(), token) {
				t.Fatal("response exposed token")
			}
			if len(response.Result().Cookies()) != 0 {
				t.Fatal("viewer issued a cookie that would reach other local ports")
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatal("rejected requests reached the guest")
	}
}

func TestViewerHTTPAndWebSocketRoundTrip(t *testing.T) {
	guest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Sec-WebSocket-Protocol") != "binary" {
			t.Error("host credentials reached the guest")
		}
		if r.URL.Path != "/websockify" {
			t.Error("host browser requested guest code")
			http.Error(w, "guest code is not a viewer asset", 500)
			return
		}
		connection, buffer, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer connection.Close()
		fmt.Fprint(buffer, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Protocol: binary\r\n\r\n")
		if err := buffer.Flush(); err != nil {
			t.Error(err)
			return
		}
		_, _ = io.Copy(connection, buffer)
	}))
	defer guest.Close()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	token, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	viewer := Server{Host: listener.Addr().String(), Token: token, Dial: func(ctx context.Context) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", guest.Listener.Addr().String())
	}}
	server := &http.Server{Handler: viewer.Handler()}
	go server.Serve(listener)
	defer server.Close()
	request, err := http.NewRequest("GET", "http://"+viewer.Host+"/assets/viewer.js", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || !strings.Contains(string(data), "import RFB") {
		t.Fatalf("proxy response: %q, %v", data, err)
	}
	request, _ = http.NewRequest("GET", "http://"+viewer.Host+"/guest-script.js", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatal("guest script route was exposed")
	}
	connection, err := net.Dial("tcp", viewer.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if deadline, ok := t.Deadline(); ok {
		connection.SetDeadline(deadline)
	}
	fmt.Fprintf(connection, "GET /websockify HTTP/1.1\r\nHost: %s\r\nOrigin: http://%s\r\nCookie: unrelated=value\r\nAuthorization: unrelated\r\nSec-WebSocket-Protocol: binary, cooper-auth-%s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n", viewer.Host, viewer.Host, token)
	reader := bufio.NewReader(connection)
	response, err = http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 101 || response.Header.Get("Sec-WebSocket-Protocol") != "binary" {
		t.Fatalf("upgrade: %s", response.Status)
	}
	const payload = "display input and output"
	if _, err := io.WriteString(connection, payload); err != nil {
		t.Fatal(err)
	}
	data = make([]byte, len(payload))
	if _, err := io.ReadFull(reader, data); err != nil {
		t.Fatal(err)
	}
	if string(data) != payload {
		t.Fatalf("websocket data changed: %q", data)
	}
}

func TestViewerCapabilitiesAreSeparate(t *testing.T) {
	first, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	viewer := Server{Host: "127.0.0.1:12345", Token: second}
	request := httptest.NewRequest("GET", "http://"+viewer.Host+"/health", nil)
	request.Header.Set("Authorization", "Bearer "+first)
	response := httptest.NewRecorder()
	viewer.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatal("one viewer accepted another viewer's capability")
	}
}
