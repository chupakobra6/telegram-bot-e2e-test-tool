package mtproto

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gotd/td/tg"
)

func TestResolveProxyURLPrefersHTTPProxy(t *testing.T) {
	clearProxyEnv(t)
	t.Setenv("ALL_PROXY", "")
	t.Setenv("all_proxy", "")
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("https_proxy", "")
	t.Setenv("HTTP_PROXY", "http://proxy.local:8080")
	t.Setenv("http_proxy", "")

	proxyURL, err := resolveProxyURL("149.154.167.50:443")
	if err != nil {
		t.Fatalf("resolve proxy: %v", err)
	}
	if proxyURL == nil {
		t.Fatal("expected proxy URL")
	}
	if got, want := proxyURL.String(), "http://proxy.local:8080"; got != want {
		t.Fatalf("proxy URL mismatch: got %q want %q", got, want)
	}
}

func TestResolveProxyURLRespectsNoProxy(t *testing.T) {
	clearProxyEnv(t)
	t.Setenv("ALL_PROXY", "")
	t.Setenv("all_proxy", "")
	t.Setenv("HTTP_PROXY", "http://proxy.local:8080")
	t.Setenv("http_proxy", "")
	t.Setenv("NO_PROXY", "localhost,127.0.0.1,.telegram.org")
	t.Setenv("no_proxy", "")

	cases := []string{
		"localhost:9000",
		"127.0.0.1:443",
		"api.telegram.org:443",
	}
	for _, addr := range cases {
		proxyURL, err := resolveProxyURL(addr)
		if err != nil {
			t.Fatalf("resolve proxy for %q: %v", addr, err)
		}
		if proxyURL != nil {
			t.Fatalf("expected no proxy for %q, got %v", addr, proxyURL)
		}
	}
}

func TestDialHTTPConnectProxy(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	requests := make(chan *http.Request, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		req, err := http.ReadRequest(reader)
		if err != nil {
			return
		}
		requests <- req
		_, _ = fmt.Fprint(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
	}()

	proxyURL, err := url.Parse("http://user:pass@" + listener.Addr().String())
	if err != nil {
		t.Fatalf("parse proxy url: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, err := dialHTTPConnectProxy(ctx, "tcp", "149.154.167.50:443", proxyURL)
	if err != nil {
		t.Fatalf("dial via proxy: %v", err)
	}
	defer conn.Close()

	select {
	case req := <-requests:
		if got, want := req.Method, http.MethodConnect; got != want {
			t.Fatalf("method mismatch: got %q want %q", got, want)
		}
		if got, want := req.Host, "149.154.167.50:443"; got != want {
			t.Fatalf("host mismatch: got %q want %q", got, want)
		}
		wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:pass"))
		if got := req.Header.Get("Proxy-Authorization"); got != wantAuth {
			t.Fatalf("proxy auth mismatch: got %q want %q", got, wantAuth)
		}
	case <-ctx.Done():
		t.Fatal("did not observe connect request")
	}
}

func TestShouldBypassProxyMatchesCommonPatterns(t *testing.T) {
	clearProxyEnv(t)
	t.Setenv("NO_PROXY", "localhost,127.0.0.1,.telegram.org,10.0.0.0/8")
	t.Setenv("no_proxy", "")

	cases := []struct {
		host string
		want bool
	}{
		{host: "localhost", want: true},
		{host: "127.0.0.1", want: true},
		{host: "api.telegram.org", want: true},
		{host: "149.154.167.50", want: false},
		{host: "10.1.2.3", want: true},
	}

	for _, tc := range cases {
		t.Run(strings.ReplaceAll(tc.host, ".", "_"), func(t *testing.T) {
			if got := shouldBypassProxy(tc.host); got != tc.want {
				t.Fatalf("shouldBypassProxy(%q) = %v, want %v", tc.host, got, tc.want)
			}
		})
	}
}

func TestDocumentKindAndPlaceholder(t *testing.T) {
	voice := &tg.MessageMediaDocument{
		Voice: true,
	}
	if got := documentKind(voice); got != "voice" {
		t.Fatalf("documentKind(voice) = %q", got)
	}
	if got := documentPlaceholder(voice); got != "[voice]" {
		t.Fatalf("documentPlaceholder(voice) = %q", got)
	}

	audio := &tg.MessageMediaDocument{}
	audio.SetDocument(&tg.Document{
		Attributes: []tg.DocumentAttributeClass{
			&tg.DocumentAttributeAudio{},
		},
	})
	if got := documentKind(audio); got != "audio" {
		t.Fatalf("documentKind(audio) = %q", got)
	}
	if got := documentPlaceholder(audio); got != "[audio]" {
		t.Fatalf("documentPlaceholder(audio) = %q", got)
	}
}

func clearProxyEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"ALL_PROXY", "all_proxy",
		"HTTP_PROXY", "http_proxy",
		"HTTPS_PROXY", "https_proxy",
		"NO_PROXY", "no_proxy",
	} {
		t.Setenv(key, "")
	}
}
