// Package desktop serves a local, authenticated view of one workload display.
package desktop

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

// Dial connects only to the desktop selected by the host launch command.
type Dial func(context.Context) (net.Conn, error)

type Server struct {
	Host  string
	Token string
	Dial  Dial
}

func NewToken() (string, error) {
	var data [32]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(data[:]), nil
}

// Handler checks local authority before it opens any guest stream. The token
// enters through a URL fragment, so browser requests and access logs omit it.
func (s Server) Handler() http.Handler {
	target := &url.URL{Scheme: "http", Host: "cooper-desktop"}
	proxy := httputil.NewSingleHostReverseProxy(target)
	transport := &http.Transport{
		DialContext:  func(ctx context.Context, _, _ string) (net.Conn, error) { return s.Dial(ctx) },
		MaxIdleConns: 16, MaxIdleConnsPerHost: 16, MaxConnsPerHost: 32,
		IdleConnTimeout: 30 * time.Second, ResponseHeaderTimeout: 15 * time.Second,
	}
	proxy.Transport = transport
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, "The desktop connection is closed. Run the Cooper launch command to reconnect.", http.StatusBadGateway)
	}
	assets := assetHandler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self'; img-src 'self' data:; object-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		if r.Host != s.Host {
			http.Error(w, "Invalid desktop host.", http.StatusForbidden)
			return
		}
		origin := r.Header.Get("Origin")
		if origin != "" && origin != "http://"+s.Host {
			http.Error(w, "Invalid desktop origin.", http.StatusForbidden)
			return
		}
		if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" {
			http.Error(w, "Cross-site desktop access is not allowed.", http.StatusForbidden)
			return
		}
		if r.URL.Path == "/" && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			data, _ := viewerAssets.ReadFile("assets/index.html")
			_, _ = w.Write(data)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			assets.ServeHTTP(w, r)
			return
		}
		if !s.authorized(r) {
			http.Error(w, "Open the private desktop link printed by Cooper.", http.StatusUnauthorized)
			return
		}
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") && origin != "http://"+s.Host {
			http.Error(w, "A desktop WebSocket needs its viewer origin.", http.StatusForbidden)
			return
		}
		if r.URL.Path == "/health" {
			_, _ = io.WriteString(w, "ready")
			return
		}
		if r.URL.Path != "/websockify" || r.Method != http.MethodGet || !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			http.NotFound(w, r)
			return
		}
		// The guest does not need the host's viewer capability.
		r.Header.Del("Cookie")
		r.Header.Del("Authorization")
		r.Header.Set("Sec-WebSocket-Protocol", "binary")
		proxy.ServeHTTP(w, r)
	})
}

func (s Server) authorized(r *http.Request) bool {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		return ok && equalToken(token, s.Token)
	}
	// Browser WebSockets cannot set Authorization. Use an offered protocol
	// for the capability, then remove it before the guest connection. Cookies
	// are not safe here: browsers send them to every port on the same host.
	var binary, authenticated bool
	for _, header := range r.Header.Values("Sec-WebSocket-Protocol") {
		for _, protocol := range strings.Split(header, ",") {
			protocol = strings.TrimSpace(protocol)
			binary = binary || protocol == "binary"
			token, ok := strings.CutPrefix(protocol, "cooper-auth-")
			authenticated = authenticated || (ok && equalToken(token, s.Token))
		}
	}
	return binary && authenticated
}

func equalToken(value, expected string) bool {
	return len(expected) == 64 && subtle.ConstantTimeCompare([]byte(value), []byte(expected)) == 1
}

func (s Server) URL() string { return fmt.Sprintf("http://%s/#%s", s.Host, s.Token) }
