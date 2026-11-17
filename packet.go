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

// Packet OpCodes
const (
	PacketMasterLogin    = 0x0064
	PacketCharLogin      = 0x0065
	PacketCharSelect     = 0x0066
	PacketMapLogin       = 0x0436 // LunarisRO / Rozclub 20250716 ENTER2 (23 bytes)
	PacketMapLoaded      = 0x007D // CZ.NOTIFY_ACTORINIT
	PacketBlockingCancel = 0x0447 // CZ.BLOCKING_PLAY_CANCEL
	PacketMove           = 0x035F
	PacketAttack         = 0x0437
	PacketTakeItem       = 0x0362
	PacketSync           = 0x0360
	PacketPingLive       = 0x0B1C
	PacketRozSend        = 0x0CF0
	PacketRozRecv        = 0x0CF1
)

const (
	OpConfig byte = 1
	OpPing   byte = 2
	OpStart  byte = 3
	OpStop   byte = 4
)

func BuildMasterLogin(version uint32, username, password string, masterVersion byte) []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint16(PacketMasterLogin))
	binary.Write(buf, binary.LittleEndian, version)

	userBytes := make([]byte, 24)
	copy(userBytes, []byte(username))
	buf.Write(userBytes)

	passBytes := make([]byte, 24)
	copy(passBytes, []byte(password))
	buf.Write(passBytes)

	buf.WriteByte(masterVersion)
	return buf.Bytes()
}

func BuildCharLogin(accountID, sessionID1, sessionID2 uint32, sex byte) []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint16(PacketCharLogin))
	binary.Write(buf, binary.LittleEndian, accountID)
	binary.Write(buf, binary.LittleEndian, sessionID1)
	binary.Write(buf, binary.LittleEndian, sessionID2)
	binary.Write(buf, binary.LittleEndian, uint16(0))
	buf.WriteByte(sex)
	return buf.Bytes()
}

func BuildCharSelect(slot byte) []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint16(PacketCharSelect))
	buf.WriteByte(slot)
	return buf.Bytes()
}

// BuildMapLogin (0x0436, 23 bytes)
// Opcode (2) + AccountID (4) + CharID (4) + SessionID (4) + Unknown (4) + Tick (4) + Sex (1) = 23 bytes
func BuildMapLogin(accountID, charID, sessionID uint32, sex byte) []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint16(PacketMapLogin))
	binary.Write(buf, binary.LittleEndian, accountID)
	binary.Write(buf, binary.LittleEndian, charID)
	binary.Write(buf, binary.LittleEndian, sessionID)
	binary.Write(buf, binary.LittleEndian, uint32(0)) // unknown (4 bytes)
	tick := uint32(time.Now().UnixMilli())
	binary.Write(buf, binary.LittleEndian, tick)
	buf.WriteByte(sex)
	return buf.Bytes()
}

func BuildMapLoaded() []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint16(PacketMapLoaded))
	return buf.Bytes()
}

func BuildBlockingCancel() []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint16(PacketBlockingCancel))
	return buf.Bytes()
}

func BuildMove(x, y int) []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint16(PacketMove))
	var c0 = byte((x >> 2) & 0xFF)
	var c1 = byte(((x & 0x03) << 6) | ((y >> 4) & 0x3F))
	var c2 = byte((y & 0x0F) << 4)
	buf.Write([]byte{c0, c1, c2})
	return buf.Bytes()
}

func BuildAttack(targetGID uint32) []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint16(PacketAttack))
	binary.Write(buf, binary.LittleEndian, targetGID)
	buf.WriteByte(7) // 7 = Continuous attack
	return buf.Bytes()
}

func BuildTakeItem(itemGID uint32) []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint16(PacketTakeItem))
	binary.Write(buf, binary.LittleEndian, itemGID)
	return buf.Bytes()
}

func BuildSync(tick uint32) []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint16(PacketSync))
	binary.Write(buf, binary.LittleEndian, tick)
	return buf.Bytes()
}

func BuildPingLive() []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint16(PacketPingLive))
	return buf.Bytes()
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

// Stat ID Constants
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

// BuildTalk (0x0090 CZ_CONTACTNPC: 7 bytes: 2B header + 4B GID + 1B Type)
func BuildTalk(npcGID uint32) []byte {
	pkt := make([]byte, 7)
	binary.LittleEndian.PutUint16(pkt[0:2], 0x0090)
	binary.LittleEndian.PutUint32(pkt[2:6], npcGID)
	pkt[6] = 1
	return pkt
}

// BuildTalkContinue (0x00B9 CZ_REQNAME: 6 bytes: 2B header + 4B GID)
func BuildTalkContinue(npcGID uint32) []byte {
	pkt := make([]byte, 6)
	binary.LittleEndian.PutUint16(pkt[0:2], 0x00B9)
	binary.LittleEndian.PutUint32(pkt[2:6], npcGID)
	return pkt
}

// BuildTalkResponse (0x00B8 CZ_REQ_NEXT_SCRIPT: 7 bytes: 2B header + 4B GID + 1B SelectIndex)
func BuildTalkResponse(npcGID uint32, response byte) []byte {
	pkt := make([]byte, 7)
	binary.LittleEndian.PutUint16(pkt[0:2], 0x00B8)
	binary.LittleEndian.PutUint32(pkt[2:6], npcGID)
	pkt[6] = response
	return pkt
}

// BuildTalkCancel (0x0146 / 0x00B8 cancel: 6 bytes: 2B header + 4B GID)
func BuildTalkCancel(npcGID uint32) []byte {
	pkt := make([]byte, 6)
	binary.LittleEndian.PutUint16(pkt[0:2], 0x0146)
	binary.LittleEndian.PutUint32(pkt[2:6], npcGID)
	return pkt
}

// BuildRestart (0x00B2 CZ_RESTART: 3 bytes: 2B header + 1B Type, Type 0 = Respawn to Save Point)
func BuildRestart(restartType byte) []byte {
	return []byte{0xB2, 0x00, restartType}
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

// SolveClientGuard calculates the Rozclub client integrity response for challenge c
func SolveClientGuard(cHex string) string {
	var edi uint32
	fmt.Sscanf(cHex, "%x", &edi)

	key := []byte("R0z-TH:9f1c-77ab-Guard!k3y#2026")
	const prime = uint32(0x1000193)

	// Pass 1: FNV-1a on Secret Key
	edx := uint32(0x811c9dc5)
	for _, b := range key {
		edx = (edx ^ uint32(b)) * prime
	}

	// Pass 2: FNV-1a on 4 bytes of challenge
	for i := 0; i < 4; i++ {
		b := uint32((edi >> (i * 8)) & 0xFF)
		edx = (edx ^ b) * prime
	}

	// Pass 3: FNV-1a on Secret Key XOR 0x5A
	for _, b := range key {
		edx = (edx ^ uint32(b^0x5A)) * prime
	}

	// Pass 4: Final mixing
	eax := edx >> 15
	edx = (edx ^ eax) * 0x2C1B3C6D
	eax = edx >> 12
	eax = (eax ^ edx) * 0x297A2D39
	edx = eax >> 15
	r := eax ^ edx

	// Constants matching real client environment:
	// h = 0x838FA736 (local hardware ID)
	// d = 0x796F19F8 (FNV-1a checksum of genuine d3d9.dll)
	const hVal = uint32(0x838FA736)
	const dVal = uint32(0x796F19F8)

	return fmt.Sprintf("r=%08X\nh=%08X\nv=3\nd=%08X\n", r, hVal, dVal)
}

// BuildGuardResponse builds the 0x0CF0 type 0x1E response packet
func BuildGuardResponse(cHex string) []byte {
	payload := SolveClientGuard(cHex)
	return BuildRozPacket(0x1E, payload)
}


