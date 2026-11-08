package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"nhooyr.io/websocket"
)

type WsConn struct {
	conn *websocket.Conn
	ctx  context.Context
}

// DialWs เชื่อมต่อตรงไปยัง WSS Tunnel ของ Rozclub โดยไม่ต้องใช้บริดจ์
func DialWs(port int) (*WsConn, error) {
	targetHost := "play.rozclub.com"
	wsURL := fmt.Sprintf("wss://%s/ws/127.0.0.1:%d", targetHost, port)

	opts := &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Origin":     []string{fmt.Sprintf("https://%s", targetHost)},
			"User-Agent": []string{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, opts)
	if err != nil {
		return nil, fmt.Errorf("dial WSS failed (%s): %w", wsURL, err)
	}

	// Unlimit read message size
	conn.SetReadLimit(65536)

	wsConn := &WsConn{
		conn: conn,
		ctx:  context.Background(),
	}
	wsConn.StartTransportPing()
	return wsConn, nil
}

func (w *WsConn) StartTransportPing() {
	go func() {
		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-w.ctx.Done():
				return
			case <-ticker.C:
				pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = w.conn.Ping(pingCtx)
				cancel()
			}
		}
	}()
}

func (w *WsConn) Write(p []byte) (int, error) {
	ctx, cancel := context.WithTimeout(w.ctx, 5*time.Second)
	defer cancel()

	err := w.conn.Write(ctx, websocket.MessageBinary, p)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (w *WsConn) Read() ([]byte, error) {
	_, data, err := w.conn.Read(w.ctx)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (w *WsConn) Close() error {
	return w.conn.Close(websocket.StatusNormalClosure, "disconnecting")
}
