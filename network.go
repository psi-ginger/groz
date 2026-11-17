package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"time"
)

func (b *SoloBot) stepMap() error {
	addr := fmt.Sprintf("%s:%d", b.cfg.Host, b.mapPort)
	conn, err := net.DialTimeout("tcp", addr, 8*time.Second)
	if err != nil {
		return err
	}
	b.mapConn = conn
	defer conn.Close()

	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.SetNoDelay(true)
		_ = tcpConn.SetKeepAlive(true)
	}

	time.Sleep(200 * time.Millisecond)

	// Send 0x0436 (MapLogin) - 23 bytes
	mapLoginPkt := BuildMapLogin(b.accountID, b.charID, b.sessionID1, b.sex)
	if len(mapLoginPkt) >= 19 {
		key17 := mapLoginPkt[2:19]
		b.clientCipher = NewEc('C', key17)
		b.serverCipher = NewEc('S', key17)
	}

	fmt.Printf("      -> ส่ง MapLogin (0x0436) ขนาด %d bytes (AccountID: %d, CharID: %d)...\n", len(mapLoginPkt), b.accountID, b.charID)
	if _, err := conn.Write(mapLoginPkt); err != nil {
		return err
	}

	// Watchdog Timer สำหรับตรวจสอบการแลกเปลี่ยน และส่ง Keep-alive Sync (0x0360) ทุก 30 วินาที
	stopWatchdog := make(chan struct{})
	defer close(stopWatchdog)
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopWatchdog:
				return
			case <-ticker.C:
				if b.cipherActive {
					syncPkt := BuildSync(uint32(time.Now().UnixMilli()))
					_ = b.sendMapPacket(syncPkt)
					if b.trade != nil {
						b.trade.CheckWatchdog()
					}
				}
			}
		}
	}()

	// Start GPS & Movement Goroutine
	stopGPS := make(chan struct{})
	defer close(stopGPS)
	go b.startGPSLoop(stopGPS)

	// Start Cloud Unified Web HUD Pusher
	stopHUD := make(chan struct{})
	defer close(stopHUD)
	go b.StartHUDPusher(stopHUD)

	readBuf := make([]byte, 65536)
	var streamBuf []byte
	b.cipherActive = false

	for b.running {
		n, err := conn.Read(readBuf)
		if err != nil {
			return fmt.Errorf("map connection closed: %w", err)
		}
		raw := make([]byte, n)
		copy(raw, readBuf[:n])

		if b.cipherActive && b.serverCipher != nil {
			b.serverCipher.Xor(raw)
		}

		streamBuf = append(streamBuf, raw...)

		for len(streamBuf) >= 2 {
			if !b.cipherActive {
				if len(streamBuf) >= 13 {
					pktID := binary.LittleEndian.Uint16(streamBuf[0:2])
					if pktID == 0x0CF1 && string(streamBuf[4:12]) == "THAA#9!Z" && streamBuf[12] == 40 {
						pktLen := int(binary.LittleEndian.Uint16(streamBuf[2:4]))
						if len(streamBuf) < pktLen {
							break
						}
						b.cipherActive = true
						fmt.Printf("\n[3/4] 🛡️ [CIPHER ACTIVATED] ตรวจพบ Handshake 0x0CF1 จาก Map Server %d! ปลดล็อก Stream Cipher สำเร็จ!\n", b.mapPort)

						remaining := streamBuf[pktLen:]
						if len(remaining) > 0 && b.serverCipher != nil {
							b.serverCipher.Xor(remaining)
						}
						streamBuf = remaining
						continue
					}
				}
				if len(streamBuf) > 0 && streamBuf[0] != 0xF1 {
					streamBuf = streamBuf[1:]
					continue
				}
				break
			}

			pktID := binary.LittleEndian.Uint16(streamBuf[0:2])
			pLen, exists := b.packetLens[pktID]
			if !exists {
				if streamBuf[0] == 0x19 && streamBuf[1] == 0x0B {
					_ = b.sendMapPacket(BuildPingLive())
					fmt.Println("[DBG-trace] ⚡ ตอบกลับ PingLive (0x0B1C)")
					streamBuf = streamBuf[2:]
					continue
				}
				fmt.Printf("[DBG-unknown] ⚠️ ไม่พบในตาราง: 0x%04X (byte0=%02X byte1=%02X)\n", pktID, streamBuf[0], streamBuf[1])
				streamBuf = streamBuf[1:]
				continue
			}

			actualLen := pLen
			if pLen == -1 {
				if len(streamBuf) < 4 {
					break
				}
				actualLen = int(binary.LittleEndian.Uint16(streamBuf[2:4]))
				if actualLen < 4 {
					actualLen = 4
				}
			}

			if len(streamBuf) < actualLen {
				break
			}

			packet := streamBuf[:actualLen]
			streamBuf = streamBuf[actualLen:]

			if pktID != 0x007F {
				fmt.Printf("[DBG-pkt] 📥 0x%04X (len %d)\n", pktID, actualLen)
			}

			b.handleMapPacket(pktID, packet)
		}
	}

	return nil
}
