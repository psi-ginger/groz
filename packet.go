package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"time"
)

// LoadPacketLengths โหลดตารางความยาวของทุกแพ็กเก็ตจาก recvpackets.txt เพื่อแกะสตรีมเน็ตเวิร์กได้ถูกต้อง 100%
func LoadPacketLengths(paths ...string) map[uint16]int {
	table := make(map[uint16]int)
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				var op uint16
				var l int
				fmt.Sscanf(parts[0], "%04x", &op)
				fmt.Sscanf(parts[1], "%d", &l)
				if op > 0 {
					table[op] = l
				}
			}
		}
		if len(table) > 0 {
			break
		}
	}
	return table
}

// Packet IDs
const (
	PacketMasterLogin = 0x0064
	PacketCharLogin   = 0x0065
	PacketCharSelect  = 0x0066
	PacketMapLogin    = 0x0436 // LunarisRO / Rozclub 20250716 ENTER2
	PacketMapLoaded   = 0x007D // CZ.NOTIFY_ACTORINIT (แจ้งแมพว่าโหลดเสร็จแล้ว)
	PacketBlockingCancel = 0x0447 // CZ.BLOCKING_PLAY_CANCEL (ปลดล็อคตัวละครให้ขยับและตีมอนได้)
	PacketRozSend     = 0x0CF0 // 3312: Rozclub In-Game Auto Bot
	PacketRozRecv     = 0x0CF1 // 3313: Rozclub In-Game Auto Bot Response
)

// OpCodes for Packet 0x0CF0
const (
	OpConfig byte = 1 // บันทึกการตั้งค่า
	OpPing   byte = 2 // Heartbeat เช็คสถานะ
	OpStart  byte = 3 // สั่งเปิดบอท ("full" = ล่าอัตโนมัติ, "support" = ปั๊มยา)
	OpStop   byte = 4 // สั่งปิดบอท ("")
)

// BuildMasterLogin (0x0064, 55 bytes)
func BuildMasterLogin(version uint32, username, password string, masterVersion byte) []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint16(PacketMasterLogin))
	binary.Write(buf, binary.LittleEndian, version) // 55 for Rozclub

	userBytes := make([]byte, 24)
	copy(userBytes, []byte(username))
	buf.Write(userBytes)

	passBytes := make([]byte, 24)
	copy(passBytes, []byte(password))
	buf.Write(passBytes)

	buf.WriteByte(masterVersion) // 0
	return buf.Bytes()
}

// BuildCharLogin (0x0065, 17 bytes)
func BuildCharLogin(accountID, sessionID1, sessionID2 uint32, sex byte) []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint16(PacketCharLogin))
	binary.Write(buf, binary.LittleEndian, accountID)
	binary.Write(buf, binary.LittleEndian, sessionID1)
	binary.Write(buf, binary.LittleEndian, sessionID2)
	binary.Write(buf, binary.LittleEndian, uint16(0)) // userLevel (2 bytes)
	buf.WriteByte(sex)                              // sex (1 byte)
	return buf.Bytes()
}

// BuildCharSelect (0x0066, 3 bytes)
func BuildCharSelect(slot byte) []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint16(PacketCharSelect))
	buf.WriteByte(slot)
	return buf.Bytes()
}

// BuildMapLogin (0x0436, 23 bytes)
// AccountID (4) + CharID (4) + AuthCode/SessionID (4) + Unknown (4) + Tick (4) + Sex (1) = 23 bytes
func BuildMapLogin(accountID, charID, sessionID uint32, sex byte) []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint16(PacketMapLogin))
	binary.Write(buf, binary.LittleEndian, accountID)
	binary.Write(buf, binary.LittleEndian, charID)
	binary.Write(buf, binary.LittleEndian, sessionID)
	binary.Write(buf, binary.LittleEndian, uint32(0)) // unknown
	tick := uint32(time.Now().UnixMilli())
	binary.Write(buf, binary.LittleEndian, tick)
	buf.WriteByte(sex)
	return buf.Bytes()
}

// BuildMapLoaded (0x007D, 2 bytes)
func BuildMapLoaded() []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint16(PacketMapLoaded))
	return buf.Bytes()
}

// BuildBlockingPlayCancel (0x0447, 2 bytes)
// ปลดล็อคสถานะแช่แข็งหน้าจอ ให้เซิร์ฟเวอร์อนุญาตให้ตัวละครเดินและตีมอนได้
func BuildBlockingPlayCancel() []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint16(PacketBlockingCancel))
	return buf.Bytes()
}

// BuildRozPacket (0x0CF0, 9 + N bytes)
func BuildRozPacket(op byte, payload string) []byte {
	payloadBytes := []byte(payload)
	totalLen := uint16(9 + len(payloadBytes))

	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint16(PacketRozSend))
	binary.Write(buf, binary.LittleEndian, totalLen)
	buf.WriteString("THAA")
	buf.WriteByte(op)
	buf.Write(payloadBytes)

	return buf.Bytes()
}

const (
	PacketPingLiveServer = 0x0B19 // ZC_PING_LIVE (2 bytes)
	PacketPingLiveClient = 0x0B1C // CZ_PING_LIVE (2 bytes)
)

// BuildPingLive (0x0B1C, 2 bytes) ตอบกลับ ZC_PING_LIVE ทันที
func BuildPingLive() []byte {
	return []byte{0x1C, 0x0B}
}

// BuildSync (0x0360, 6 bytes: 2 bytes header + 4 bytes tick)
// จำเป็นอย่างยิ่งสำหรับเลี้ยงสาย Map Server ไม่ให้โดน Timeout ตัดหลุด
func BuildSync(tick uint32) []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint16(0x0360))
	binary.Write(buf, binary.LittleEndian, tick)
	return buf.Bytes()
}

// BuildMove (0x035F, 5 bytes: 2 bytes header + 3 bytes packed coords)
// หมายเหตุ: ใน Online.js ของ Rozclub (PacketVer >= 20180307) ใช้ REQUEST_MOVE2 (863 = 0x035F)
func BuildMove(x, y int) []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint16(0x035F))

	// RO 24-bit Coordinate Packing: 10 bits X + 10 bits Y + 4 bits padding
	b0 := byte((x >> 2) & 0xFF)
	b1 := byte(((x & 0x03) << 6) | ((y >> 4) & 0x3F))
	b2 := byte((y & 0x0F) << 4)

	buf.Write([]byte{b0, b1, b2})
	return buf.Bytes()
}

func DumpHex(data []byte) string {
	var s string
	for i, b := range data {
		if i > 0 {
			s += " "
		}
		s += fmt.Sprintf("%02X", b)
	}
	return s
}

// BuildDealInitiate (0x00E4, 6 bytes: 2 bytes header + 4 bytes Target GID)
func BuildDealInitiate(targetGID uint32) []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint16(0x00E4))
	binary.Write(buf, binary.LittleEndian, targetGID)
	return buf.Bytes()
}

// BuildDealReply (0x00E6, 3 bytes: 2 bytes header + 1 byte action: 1 = accept, 0 = refuse)
func BuildDealReply(action byte) []byte {
	return []byte{0xE6, 0x00, action}
}

// BuildDealAddItem (0x00E8, 8 bytes: 2 bytes header + 2 bytes Index + 4 bytes Amount)
func BuildDealAddItem(index uint16, amount uint32) []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint16(0x00E8))
	binary.Write(buf, binary.LittleEndian, index)
	binary.Write(buf, binary.LittleEndian, amount)
	return buf.Bytes()
}

// BuildDealFinalize (0x00EB, 2 bytes: Lock ดีล)
func BuildDealFinalize() []byte {
	return []byte{0xEB, 0x00}
}

// BuildDealTrade (0x00EF, 2 bytes: ยืนยันจบเทรด OK)
func BuildDealTrade() []byte {
	return []byte{0xEF, 0x00}
}

// BuildDealCancel (0x00ED, 2 bytes: ยกเลิกดีล)
func BuildDealCancel() []byte {
	return []byte{0xED, 0x00}
}

// BuildWhisper (0x0096 CZ_WHISPER: 2 bytes ID + 2 bytes len + 24 bytes Recipient + Message + 0x00)
func BuildWhisper(recipient, message string) []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint16(0x0096))

	msgBytes := []byte(message)
	pktLen := uint16(2 + 2 + 24 + len(msgBytes) + 1)
	binary.Write(buf, binary.LittleEndian, pktLen)

	nameBytes := make([]byte, 24)
	copy(nameBytes, []byte(recipient))
	buf.Write(nameBytes)

	buf.Write(msgBytes)
	buf.WriteByte(0x00) // Null-terminator
	return buf.Bytes()
}

// BuildUseItem (0x0439 CZ_USE_ITEM: 2 bytes ID + 2 bytes Index)
func BuildUseItem(index uint16) []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint16(0x0439))
	binary.Write(buf, binary.LittleEndian, index)
	return buf.Bytes()
}

// BuildRestart (0x00B2 CZ_RESTART: 2 bytes header + 1 byte type: 0 = กลับจุดเซฟ)
func BuildRestart(respawnType byte) []byte {
	return []byte{0xB2, 0x00, respawnType}
}

const (
	StatSTR = 13 // 0x0D
	StatAGI = 14 // 0x0E
	StatVIT = 15 // 0x0F
	StatINT = 16 // 0x10
	StatDEX = 17 // 0x11
	StatLUK = 18 // 0x12
)

// BuildAddStatus (0x00BB CZ_STATUS_CHANGE: 5 bytes: 2B header + 2B statID + 1B amount)
func BuildAddStatus(statID uint16) []byte {
	pkt := make([]byte, 5)
	binary.LittleEndian.PutUint16(pkt[0:2], 0x00BB)
	binary.LittleEndian.PutUint16(pkt[2:4], statID)
	pkt[4] = 1
	return pkt
}

// BuildAddSkill (0x0112 CZ_UPGRADE_SKILLLEVEL: 4 bytes: 2B header + 2B skillID)
func BuildAddSkill(skillID uint16) []byte {
	pkt := make([]byte, 4)
	binary.LittleEndian.PutUint16(pkt[0:2], 0x0112)
	binary.LittleEndian.PutUint16(pkt[2:4], skillID)
	return pkt
}
