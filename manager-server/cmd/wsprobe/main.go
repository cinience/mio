package main

import (
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type headerList []string

func (h *headerList) String() string {
	if h == nil {
		return ""
	}
	return strings.Join(*h, ", ")
}

func (h *headerList) Set(value string) error {
	*h = append(*h, value)
	return nil
}

type stringList []string

func (s *stringList) String() string {
	if s == nil {
		return ""
	}
	return strings.Join(*s, ", ")
}

func (s *stringList) Set(value string) error {
	if value == "" {
		return nil
	}
	*s = append(*s, value)
	return nil
}

type probeResult struct {
	URL           string
	Connected     bool
	HTTPStatus    string
	ResponseDebug string
	Err           error
}

func main() {
	var (
		urls        stringList
		headers     headerList
		timeout     time.Duration
		origin      string
		subprotocol string
		writePing   bool
	)

	flag.Var(&urls, "url", "WebSocket URL to test (repeatable). Defaults to common endpoints if omitted.")
	flag.Var(&headers, "header", "Additional request header in the form Key=Value (repeatable).")
	flag.DurationVar(&timeout, "timeout", 5*time.Second, "Handshake timeout.")
	flag.StringVar(&origin, "origin", "", "Optional Origin header.")
	flag.StringVar(&subprotocol, "subprotocol", "", "Optional WebSocket subprotocol.")
	flag.BoolVar(&writePing, "ping", false, "Send a ping frame after connecting.")
	flag.Parse()

	targets := urls
	if len(targets) == 0 {
		targets = append(targets,
			"ws://localhost:8002/xiaozhi/mcp_endpoint/mcp/?token=demo",
			"ws://localhost:8007/xiaozhi/mcp_endpoint/mcp/?token=demo",
		)
	}

	reqHeaders, err := buildHeaders(headers, origin, subprotocol)
	if err != nil {
		fmt.Fprintf(os.Stderr, "header error: %v\n", err)
		os.Exit(2)
	}

	results := make([]probeResult, 0, len(targets))
	for _, target := range targets {
		result := probe(target, reqHeaders, timeout, subprotocol, writePing)
		results = append(results, result)
		printResult(result)
	}

	var failed bool
	for _, r := range results {
		if !r.Connected {
			failed = true
			break
		}
	}

	if failed {
		os.Exit(1)
	}
}

func buildHeaders(raw headerList, origin, subprotocol string) (http.Header, error) {
	hdr := http.Header{}

	for _, entry := range raw {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid header %q, expected key=value", entry)
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if key == "" {
			return nil, fmt.Errorf("header key missing in %q", entry)
		}
		hdr.Add(key, value)
	}

	if origin != "" {
		hdr.Set("Origin", origin)
	}
	if subprotocol != "" {
		// We'll also set this via Dialer.Subprotocols, but keeping it in header for visibility
		hdr.Set("Sec-WebSocket-Protocol", subprotocol)
	}
	return hdr, nil
}

func probe(target string, header http.Header, timeout time.Duration, subprotocol string, sendPing bool) probeResult {
	res := probeResult{URL: target}

	parsed, err := url.Parse(target)
	if err != nil {
		res.Err = fmt.Errorf("invalid url: %w", err)
		return res
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: timeout,
		Subprotocols:     nil,
	}
	if subprotocol != "" {
		dialer.Subprotocols = []string{subprotocol}
	}

	conn, resp, err := dialer.Dial(parsed.String(), header)
	if err != nil {
		if resp != nil {
			res.HTTPStatus = resp.Status
		}
		res.Err = err
		return res
	}
	defer conn.Close()

	res.Connected = true
	if resp != nil {
		res.HTTPStatus = resp.Status
		res.ResponseDebug = formatHeaders(resp.Header)
	}

	if sendPing {
		if err := sendPingMessage(conn, timeout); err != nil {
			res.Err = fmt.Errorf("ping failed: %w", err)
			res.Connected = false
			return res
		}
	}

	// Immediately close the connection gracefully
	_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "probe"), time.Now().Add(time.Second))

	return res
}

func formatHeaders(h http.Header) string {
	if len(h) == 0 {
		return "(no headers)"
	}
	var sb strings.Builder
	for key, values := range h {
		for _, v := range values {
			sb.WriteString(fmt.Sprintf("%s: %s\n", key, v))
		}
	}
	return sb.String()
}

func sendPingMessage(conn *websocket.Conn, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	if err := conn.SetWriteDeadline(deadline); err != nil {
		return err
	}
	if err := conn.WriteControl(websocket.PingMessage, []byte("wsprobe"), deadline); err != nil {
		return err
	}
	// Optionally wait for pong to confirm round trip
	_ = conn.SetReadDeadline(deadline)
	conn.SetPongHandler(func(appData string) error {
		return nil
	})
	_, _, err := conn.ReadMessage()
	return err
}

func printResult(res probeResult) {
	fmt.Printf("=== %s ===\n", res.URL)
	if res.HTTPStatus != "" {
		fmt.Printf("HTTP status: %s\n", res.HTTPStatus)
	}
	if res.ResponseDebug != "" {
		fmt.Println("Response headers:")
		fmt.Print(res.ResponseDebug)
	}
	if res.Connected {
		fmt.Println("Connection: SUCCESS")
	} else {
		fmt.Println("Connection: FAILED")
	}
	if res.Err != nil {
		fmt.Printf("Error: %v\n", res.Err)
	}
	fmt.Println()
}
