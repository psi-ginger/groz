package main

import (
	"fmt"
	"net"
	"time"
)

const TargetServerHost = "15.235.236.242"

type WsConn struct {
	conn net.Conn
}

// DialWs เชื่อมต่อตรงไปยัง Rozclub VPS Server (15.235.236.242) ผ่าน Native TCP Socket
func DialWs(port int) (*WsConn, error) {
	addr := fmt.Sprintf("%s:%d", TargetServerHost, port)
	conn, err := net.DialTimeout("tcp", addr, 8*time.Second)
	if err != nil {
		return nil, fmt.Errorf("dial TCP failed (%s): %w", addr, err)
	}

	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.SetNoDelay(true)
		_ = tcpConn.SetKeepAlive(true)
		_ = tcpConn.SetKeepAlivePeriod(10 * time.Second)
	}

	return &WsConn{conn: conn}, nil
}

func (w *WsConn) Write(p []byte) (int, error) {
	if w.conn == nil {
		return 0, fmt.Errorf("connection is nil")
	}
	_ = w.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return w.conn.Write(p)
}

func (w *WsConn) Read() ([]byte, error) {
	if w.conn == nil {
		return nil, fmt.Errorf("connection is nil")
	}
	buf := make([]byte, 65536)
	_ = w.conn.SetReadDeadline(time.Time{}) // ไม่มี read timeout
	n, err := w.conn.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

func (w *WsConn) Close() error {
	if w.conn != nil {
		return w.conn.Close()
	}
	return nil
}
