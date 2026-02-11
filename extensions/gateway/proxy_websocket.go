package gateway

import (
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/xraph/forge"
)

// ProxyWebSocket proxies a WebSocket connection to an upstream target.
func ProxyWebSocket(
	w http.ResponseWriter,
	r *http.Request,
	route *Route,
	target *Target,
	config WebSocketConfig,
	logger forge.Logger,
) {
	// Parse upstream URL
	targetURL, err := url.Parse(target.URL)
	if err != nil {
		logger.Warn("invalid upstream URL for websocket",
			forge.F("target_url", target.URL),
			forge.F("error", err),
		)

		http.Error(w, `{"error":"invalid upstream URL"}`, http.StatusBadGateway)

		return
	}

	// Build upstream WebSocket URL
	wsScheme := "ws"
	if targetURL.Scheme == "https" {
		wsScheme = "wss"
	}

	upstreamPath := r.URL.Path
	if route.StripPrefix && route.Path != "" {
		prefix := strings.TrimSuffix(route.Path, "/*")
		upstreamPath = strings.TrimPrefix(upstreamPath, prefix)

		if upstreamPath == "" {
			upstreamPath = "/"
		}
	}

	if route.AddPrefix != "" {
		upstreamPath = route.AddPrefix + upstreamPath
	}

	upstreamURL := wsScheme + "://" + targetURL.Host + singleJoiningSlash(targetURL.Path, upstreamPath)
	if r.URL.RawQuery != "" {
		upstreamURL += "?" + r.URL.RawQuery
	}

	// Upgrade client connection
	clientConn, _, _, err := ws.UpgradeHTTP(r, w)
	if err != nil {
		logger.Warn("websocket client upgrade failed",
			forge.F("error", err),
		)

		return
	}

	defer clientConn.Close()

	// Connect to upstream
	dialer := ws.Dialer{
		Timeout: config.HandshakeTimeout,
		Header: ws.HandshakeHeaderHTTP(http.Header{
			"X-Forwarded-For":   {r.RemoteAddr},
			"X-Forwarded-Host":  {r.Host},
			"X-Forwarded-Proto": {schemeFromRequest(r)},
		}),
	}

	upstreamConn, _, _, err := dialer.Dial(r.Context(), upstreamURL)
	if err != nil {
		logger.Warn("websocket upstream connect failed",
			forge.F("upstream_url", upstreamURL),
			forge.F("error", err),
		)

		// Send close frame to client
		_ = ws.WriteFrame(clientConn, ws.NewCloseFrame(ws.NewCloseFrameBody(ws.StatusProtocolError, "upstream unavailable")))

		return
	}

	defer upstreamConn.Close()

	target.IncrConns()
	defer target.DecrConns()

	logger.Debug("websocket connection established",
		forge.F("route_id", route.ID),
		forge.F("upstream_url", upstreamURL),
	)

	// Bidirectional copy
	var wg sync.WaitGroup
	wg.Add(2)

	errCh := make(chan error, 2)

	// Client -> Upstream
	go func() {
		defer wg.Done()

		err := copyWebSocket(upstreamConn, clientConn, ws.StateServerSide)
		errCh <- err
	}()

	// Upstream -> Client
	go func() {
		defer wg.Done()

		err := copyWebSocket(clientConn, upstreamConn, ws.StateClientSide)
		errCh <- err
	}()

	// Wait for either direction to finish
	<-errCh

	// Close both connections to unblock the other goroutine
	clientConn.Close()
	upstreamConn.Close()

	wg.Wait()

	logger.Debug("websocket connection closed",
		forge.F("route_id", route.ID),
	)
}

// copyWebSocket copies WebSocket frames from src to dst.
func copyWebSocket(dst net.Conn, src net.Conn, state ws.State) error {
	for {
		header, err := ws.ReadHeader(src)
		if err != nil {
			return err
		}

		// Handle close frames
		if header.OpCode == ws.OpClose {
			// Forward close frame
			payload := make([]byte, header.Length)
			if header.Length > 0 {
				if _, err := io.ReadFull(src, payload); err != nil {
					return err
				}

				if header.Masked {
					ws.Cipher(payload, header.Mask, 0)
				}
			}

			// Send close frame to the other end
			closeFrame := ws.NewCloseFrame(ws.NewCloseFrameBody(ws.StatusNormalClosure, ""))
			if state == ws.StateServerSide {
				closeFrame = ws.MaskFrameInPlace(closeFrame)
			}

			_ = ws.WriteFrame(dst, closeFrame)

			return nil
		}

		// Handle ping/pong
		if header.OpCode == ws.OpPing {
			payload := make([]byte, header.Length)
			if header.Length > 0 {
				if _, err := io.ReadFull(src, payload); err != nil {
					return err
				}

				if header.Masked {
					ws.Cipher(payload, header.Mask, 0)
				}
			}

			// Forward ping as pong
			frame := ws.NewPongFrame(payload)
			if state == ws.StateServerSide {
				frame = ws.MaskFrameInPlace(frame)
			}

			if err := ws.WriteFrame(dst, frame); err != nil {
				return err
			}

			continue
		}

		if header.OpCode == ws.OpPong {
			// Discard pong payload
			if header.Length > 0 {
				if _, err := io.CopyN(io.Discard, src, header.Length); err != nil {
					return err
				}
			}

			continue
		}

		// Forward data frames
		payload := make([]byte, header.Length)
		if header.Length > 0 {
			if _, err := io.ReadFull(src, payload); err != nil {
				return err
			}

			if header.Masked {
				ws.Cipher(payload, header.Mask, 0)
			}
		}

		// Write frame to destination
		msg := wsutil.Message{
			OpCode:  header.OpCode,
			Payload: payload,
		}

		if state == ws.StateServerSide {
			err = wsutil.WriteClientMessage(dst, msg.OpCode, msg.Payload)
		} else {
			err = wsutil.WriteServerMessage(dst, msg.OpCode, msg.Payload)
		}

		if err != nil {
			return err
		}
	}
}

// SetWriteDeadline sets the write deadline on a connection if supported.
func setWriteDeadline(conn net.Conn, d time.Duration) {
	if d > 0 {
		_ = conn.SetWriteDeadline(time.Now().Add(d))
	}
}
