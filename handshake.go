package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"time"
)

func (b *SoloBot) stepLogin() error {
	addr := fmt.Sprintf("%s:%d", b.cfg.Host, b.cfg.Port)
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	loginPkt := BuildMasterLogin(55, b.cfg.Username, b.cfg.Password, 0)
	fmt.Printf("      -> ส่ง MasterLogin (0x0064) ขนาด %d bytes\n", len(loginPkt))
	if _, err := conn.Write(loginPkt); err != nil {
		return err
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return err
	}
	fmt.Printf("      <- ได้รับจาก Login Server: %d bytes (Hex: %s)\n", n, b.dumpHex(buf[:n]))

	if n < 2 {
		return fmt.Errorf("packet too short from login server")
	}

	pktID := binary.LittleEndian.Uint16(buf[0:2])
	if pktID == 0x006A || pktID == 0x083E {
		var reason byte = 0xFF
		if n >= 3 {
			reason = buf[2]
		}
		return fmt.Errorf("login rejected by server (code 0x%04X, reason %d)", pktID, reason)
	}

	if (pktID == 0x0AC4 || pktID == 0x0069 || pktID == 0x02B0) && n >= 47 {
		b.sessionID1 = binary.LittleEndian.Uint32(buf[4:8])
		b.accountID = binary.LittleEndian.Uint32(buf[8:12])
		b.sessionID2 = binary.LittleEndian.Uint32(buf[12:16])
		b.sex = buf[46]
		fmt.Printf("      ✓ เข้าสู่ระบบสำเร็จ! AccountID: %d, Sex: %d\n", b.accountID, b.sex)
		return nil
	}

	return fmt.Errorf("unexpected packet 0x%04X from login server", pktID)
}

func (b *SoloBot) stepChar(charPort int) error {
	addr := fmt.Sprintf("%s:%d", b.cfg.Host, charPort)
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	charLoginPkt := BuildCharLogin(b.accountID, b.sessionID1, b.sessionID2, b.sex)
	fmt.Printf("      -> ส่ง CharLogin (0x0065) ขนาด %d bytes\n", len(charLoginPkt))
	if _, err := conn.Write(charLoginPkt); err != nil {
		return err
	}

	buf := make([]byte, 8192)
	n, err := conn.Read(buf)
	if err != nil {
		return err
	}

	if n == 4 {
		fmt.Printf("      <- ได้รับ AccountID Preamble: 4 bytes (Hex: %s), กำลังรอรับ Char List...\n", b.dumpHex(buf[:n]))
		n, err = conn.Read(buf)
		if err != nil {
			return err
		}
	}

	if n < 2 {
		return fmt.Errorf("empty response from char server")
	}

	pktID := binary.LittleEndian.Uint16(buf[0:2])
	fmt.Printf("      <- ได้รับ Char List: 0x%04X (%d bytes)\n", pktID, n)

	selectedSlot := b.cfg.CharSlot
	var selectedCharID uint32
	var selectedName string

	if pktID == 0x082D || pktID == 0x09A0 || pktID == 0x006B {
		offset := 4
		for offset+175 <= n {
			block := buf[offset : offset+175]
			cid := binary.LittleEndian.Uint32(block[0:4])
			if cid > 0 {
				slot := block[145]
				jobLv := binary.LittleEndian.Uint32(block[20:24])
				baseLv := binary.LittleEndian.Uint16(block[78:80])
				nameEnd := 122
				for p := 98; p < 122; p++ {
					if block[p] == 0 {
						nameEnd = p
						break
					}
				}
				name := string(block[98:nameEnd])
				fmt.Printf("         - Slot %d: [%s] BaseLv: %d, JobLv: %d (CharID: %d)\n", slot, name, baseLv, jobLv, cid)

				if slot == selectedSlot {
					selectedCharID = cid
					selectedName = name
					b.baseLv = int(baseLv)
					b.jobLv = int(jobLv)
				}
			}
			offset += 175
		}
	}

	if selectedCharID == 0 {
		selectedCharID = b.charID
	}
	if selectedCharID == 0 {
		selectedCharID = 4265
	}

	time.Sleep(500 * time.Millisecond)

	selPkt := BuildCharSelect(selectedSlot)
	fmt.Printf("      -> ส่งคำสั่งเลือกตัวละคร Slot %d (CharID: %d, ชื่อ: %s)...\n", selectedSlot, selectedCharID, selectedName)
	if _, err := conn.Write(selPkt); err != nil {
		return err
	}

	for {
		n, err = conn.Read(buf)
		if err != nil {
			return fmt.Errorf("failed to read char select response: %w", err)
		}
		selResp := buf[:n]
		if len(selResp) < 2 {
			continue
		}
		respOp := binary.LittleEndian.Uint16(selResp[0:2])
		fmt.Printf("      <- ได้รับจาก Char Server: 0x%04X (%d bytes, Hex: %s)\n", respOp, n, b.dumpHex(selResp))

		if respOp == 0x0071 || respOp == 0x0AC5 {
			if len(selResp) >= 28 {
				b.charID = binary.LittleEndian.Uint32(selResp[2:6])
				mEnd := 22
				for p := 6; p < 22; p++ {
					if selResp[p] == 0 {
						mEnd = p
						break
					}
				}
				b.mapName = string(selResp[6:mEnd])
				b.mapPort = int(binary.LittleEndian.Uint16(selResp[26:28]))
				fmt.Printf("      ✓ ยืนยันตัวละครสำเร็จ! แผนที่: %s | Map Server พอร์ต: %d | CharID: %d\n", b.mapName, b.mapPort, b.charID)
			}
			break
		}
	}

	return nil
}
