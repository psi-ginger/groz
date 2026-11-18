package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

func (b *SoloBot) handleMapPacket(pktID uint16, packet []byte) {
	// Ping Live
	if pktID == 0x0B19 {
		_ = b.sendMapPacket(BuildPingLive())
		return
	}

	// 0x008E: Server Chat / Self Chat / Notification Message
	if pktID == 0x008E && len(packet) >= 4 {
		msg := string(bytes.Trim(packet[4:], "\x00\r\n "))
		fmt.Printf("[💬 Chat 0x008E] ข้อความ: %s\n", msg)
		return
	}

	// 0x0081: Server Disconnect / Kick Error
	if pktID == 0x0081 && len(packet) >= 3 {
		reason := packet[2]
		fmt.Printf("\n[🚨 Server Kick 0x0081] เซิร์ฟเวอร์ส่งคำสั่งตัดการเชื่อมต่อ (Reason Code: %d)!\n", reason)
		return
	}

	// 0x0091: Map Move / Warp ภายใน Server เดียวกัน
	if pktID == 0x0091 && len(packet) >= 22 {
		mapNameRaw := string(bytes.TrimRight(packet[2:18], "\x00"))
		newX := int(binary.LittleEndian.Uint16(packet[18:20]))
		newY := int(binary.LittleEndian.Uint16(packet[20:22]))
		isMapChanged := NormalizeMapName(mapNameRaw) != NormalizeMapName(b.mapName)
		b.mapName = mapNameRaw
		b.currentX = newX
		b.currentY = newY
		if isMapChanged {
			b.currentField = nil
			b.ChannelNpcFound = false
			b.ChannelNpcGID = 0
			b.ChannelTalkStep = 0
			fmt.Printf("\n[🌀 Map Move 0x0091] เปลี่ยนแมพสู่ %s ที่ (%d, %d)...\n", mapNameRaw, newX, newY)
			_ = b.sendMapPacket(BuildMapLoaded())
			_ = b.sendMapPacket(BuildBlockingCancel())
		}
		return
	}

	// 0x0092: Map Move / Server Change
	if pktID == 0x0092 && len(packet) >= 28 {
		mapNameRaw := string(bytes.TrimRight(packet[2:18], "\x00"))
		newX := int(binary.LittleEndian.Uint16(packet[18:20]))
		newY := int(binary.LittleEndian.Uint16(packet[20:22]))
		isMapChanged := NormalizeMapName(mapNameRaw) != NormalizeMapName(b.mapName)
		b.mapName = mapNameRaw
		b.currentX = newX
		b.currentY = newY
		if isMapChanged {
			b.currentField = nil
			b.ChannelNpcFound = false
			b.ChannelNpcGID = 0
			b.ChannelTalkStep = 0
			fmt.Printf("\n[🌀 Map Move 0x0092] เปลี่ยนแมพข้ามเซิร์ฟเวอร์สู่ %s ที่ (%d, %d)...\n", mapNameRaw, newX, newY)
			_ = b.sendMapPacket(BuildMapLoaded())
			_ = b.sendMapPacket(BuildBlockingCancel())
		}
		return
	}

	// พิกัดเกิด 0x0073
	if pktID == 0x0073 && len(packet) >= 11 {
		b0 := uint16(packet[6])
		b1 := uint16(packet[7])
		b2 := uint16(packet[8])
		b.currentX = int((b0 << 2) | (b1 >> 6))
		b.currentY = int(((b1 & 0x3F) << 4) | (b2 >> 4))
		fmt.Printf("[📍 พิกัด 0x0073] ยืนอยู่ที่ (%d, %d)\n", b.currentX, b.currentY)
		return
	}

	// พิกัดเกิด 0x02EB (ZC_ACCEPT_ENTER2)
	if pktID == 0x02EB && len(packet) >= 13 {
		b0 := uint16(packet[6])
		b1 := uint16(packet[7])
		b2 := uint16(packet[8])
		b.currentX = int((b0 << 2) | (b1 >> 6))
		b.currentY = int(((b1 & 0x3F) << 4) | (b2 >> 4))
		fmt.Printf("[📍 พิกัด 0x02EB] ยืนอยู่ที่ (%d, %d)\n", b.currentX, b.currentY)

		time.Sleep(100 * time.Millisecond)
		_ = b.sendMapPacket(BuildMapLoaded())
		_ = b.sendMapPacket(BuildBlockingCancel())
		fmt.Printf("[4/4] 🎮 ยืนอยู่ในแมพ %s สำเร็จเรียบร้อย! (CharID: %d) บอทกำลังออนไลน์นิ่งในเกม...\n", b.mapName, b.charID)
		return
	}

	// ก้าวเดิน 0x0087
	if pktID == 0x0087 && len(packet) >= 12 {
		pData := packet[6:12]
		toX := int(uint16(pData[2]&0x0F)<<6 | uint16(pData[3]&0xFC)>>2)
		toY := int(uint16(pData[3]&0x03)<<8 | uint16(pData[4]))
		if toX > 0 && toY > 0 && toX < 1024 && toY < 1024 {
			b.currentX = toX
			b.currentY = toY
		}
		return
	}

	// Auto-Discovery แม่ค้า & เครื่องย้ายช่อง (0x09FF, 0x0857, 0x09DD, 0x0915)
	if (pktID == 0x09FF || pktID == 0x0857 || pktID == 0x09DD || pktID == 0x0915) && len(packet) >= 9 {
		actorID := binary.LittleEndian.Uint32(packet[5:9])
		chunk := string(packet[5:])

		// ดักจับแม่ค้า
		if b.cfg.MerchantName != "" && strings.Contains(chunk, b.cfg.MerchantName) {
			if b.trade != nil && b.trade.MerchantGID != actorID {
				b.trade.MerchantGID = actorID
				fmt.Printf("\n[👀 Auto-Discovery] สแกนพบแม่ค้า '%s' ในจอ! บันทึกรหัสประจำตัว (GID: %d) ทันที\n", b.cfg.MerchantName, actorID)
			}
		}

		// ดักจับเครื่องย้ายช่อง
		if strings.Contains(chunk, "เครื่องย้ายช่อง") {
			if b.ChannelNpcGID != actorID {
				b.ChannelNpcGID = actorID
				b.ChannelNpcFound = true
				fmt.Printf("\n[📡 Auto-Discovery] ตรวจพบ 'เครื่องย้ายช่อง' ในจอ! (GID: %d)\n", actorID)
			}
		}
		return
	}

	// NPC Talk Message (0x00B4)
	if pktID == 0x00B4 && len(packet) >= 8 {
		npcID := binary.LittleEndian.Uint32(packet[4:8])
		msg := string(bytes.TrimRight(packet[8:], "\x00"))
		fmt.Printf("[💬 NPC Msg (0x00B4)] GID %d: %s\n", npcID, msg)
		return
	}

	// NPC Talk Continue / Next Button (0x00B5)
	if pktID == 0x00B5 && len(packet) >= 6 {
		npcID := binary.LittleEndian.Uint32(packet[2:6])
		fmt.Printf("[⏭️ NPC Next (0x00B5)] GID %d ส่งปุ่ม Next มา! กำลังกดยืนยัน (0x00B9)...\n", npcID)
		if b.ChannelNpcFound && (b.ChannelNpcGID == npcID || b.ChannelNpcGID == 0) {
			b.ChannelTalkStep = 1
			_ = b.sendMapPacket(BuildTalkContinue(npcID))
		}
		return
	}

	// NPC Talk Close Button (0x00B6)
	if pktID == 0x00B6 && len(packet) >= 6 {
		npcID := binary.LittleEndian.Uint32(packet[2:6])
		fmt.Printf("[❌ NPC Close (0x00B6)] ไดอะล็อก NPC GID %d ปิดลง\n", npcID)
		b.ChannelTalkStep = 0
		return
	}

	// NPC Talk Select Menu (0x00B7)
	if pktID == 0x00B7 && len(packet) >= 8 {
		npcID := binary.LittleEndian.Uint32(packet[4:8])
		menuStr := string(bytes.TrimRight(packet[8:], "\x00"))
		fmt.Printf("[📋 NPC Menu (0x00B7)] GID %d ตัวเลือก: %s\n", npcID, menuStr)
		if b.ChannelNpcFound && (b.ChannelNpcGID == npcID || b.ChannelNpcGID == 0) {
			// ตอบเลือกตัวเลือกที่ 1 ของหน้าจอ = index 2 ของ RO Protocol (ช่อง B)
			fmt.Printf("[⚡ NPC Select] ส่งคำตอบเลือก 'ช่อง B' (Menu Index 2) ไปยัง NPC %d\n", npcID)
			_ = b.sendMapPacket(BuildTalkResponse(npcID, 2))
			b.ChannelTalkStep = 0
		}
		return
	}

	// ดีลแม่ค้า 0x00E5 / 0x01F4 (คำขอดีลเข้ามา)
	if pktID == 0x00E5 || pktID == 0x01F4 {
		senderName := string(bytes.Trim(packet[2:min(len(packet), 26)], "\x00"))
		fmt.Printf("\n[🤝 Deal Request (0x%04X)] ตัวละคร '%s' ส่งคำขอดีลมา! กำลังตอบตกลง...\n", pktID, senderName)
		_ = b.sendMapPacket(BuildDealReply(3))
		if b.trade != nil {
			b.trade.DealActive = true
			b.trade.State = TradeDealWaitingHandshake
		}
		return
	}

	// 0x00E7 / 0x01F5: หน้าต่างดีลเปิด
	if pktID == 0x00E7 || pktID == 0x01F5 {
		res := packet[2]
		if res == 0 || res == 3 {
			fmt.Printf("\n[🤝 Deal Begin] หน้าต่างดีลเปิดสำเร็จ (Status: %d)!\n", res)
			if b.trade != nil {
				b.trade.HandleDealBegin()
			}
		} else {
			fmt.Printf("\n[⚠️ Deal Failed] ยื่นดีลไม่สำเร็จ (Status: %d)! รีเซ็ตสถานะ...\n", res)
			if b.trade != nil {
				b.trade.HandleDealCancel()
			}
		}
		return
	}

	// 0x00EC: อีกฝ่ายกด Lock ดีลแล้ว
	if pktID == 0x00EC {
		if b.trade != nil {
			b.trade.HandleDealOtherLocked()
		}
		return
	}

	// 0x00F0: ดีลสำเร็จสมบูรณ์
	if pktID == 0x00F0 {
		res := packet[2]
		if res == 0 {
			if b.trade != nil {
				b.trade.HandleDealComplete()
			}
		} else {
			fmt.Printf("\n[⚠️ Deal Exec Failed] ผลการแลกเปลี่ยนล้มเหลว (Status: %d)! รีเซ็ตสถานะ...\n", res)
			if b.trade != nil {
				b.trade.HandleDealCancel()
			}
		}
		return
	}

	// 0x00EE: ดีลถูกยกเลิก
	if pktID == 0x00EE {
		if b.trade != nil {
			b.trade.HandleDealCancel()
		}
		return
	}

	// ไอเทมถูกเอาออกจากตัว 0x00AF
	if pktID == 0x00AF && len(packet) >= 6 {
		idx := binary.LittleEndian.Uint16(packet[2:4])
		amount := int(binary.LittleEndian.Uint16(packet[4:6]))
		if b.Inventory != nil && idx > 0 {
			b.Inventory.RemoveItem(idx, amount)
		}
		return
	}

	// โหลดไอเทมในกระเป๋า 0x0B09
	if pktID == 0x0B09 && len(packet) >= 4 {
		itemStart := 5
		itemSize := 28
		itemBytes := packet[itemStart:]
		for i := 0; i+itemSize <= len(itemBytes); i += itemSize {
			idx := binary.LittleEndian.Uint16(itemBytes[i : i+2])
			nameID := uint16(binary.LittleEndian.Uint32(itemBytes[i+2 : i+6]))
			itemType := itemBytes[i+6]
			amount := int(binary.LittleEndian.Uint16(itemBytes[i+7 : i+9]))
			if b.Inventory != nil && idx > 0 {
				b.Inventory.SetItem(&Item{
					Index:      idx,
					NameID:     nameID,
					Amount:     amount,
					Type:       itemType,
					Identified: true,
				})
			}
		}
		return
	}

	// โหลดอุปกรณ์ 0x0B39
	if pktID == 0x0B39 && len(packet) >= 4 {
		itemStart := 5
		itemSize := 68
		itemBytes := packet[itemStart:]
		for i := 0; i+itemSize <= len(itemBytes); i += itemSize {
			idx := binary.LittleEndian.Uint16(itemBytes[i : i+2])
			nameID := uint16(binary.LittleEndian.Uint32(itemBytes[i+2 : i+6]))
			itemType := itemBytes[i+6]
			equipped := uint16(binary.LittleEndian.Uint32(itemBytes[i+11 : i+15]))
			if b.Inventory != nil && idx > 0 {
				b.Inventory.SetItem(&Item{
					Index:      idx,
					NameID:     nameID,
					Amount:     1,
					Type:       itemType,
					Equipped:   equipped,
					Identified: true,
				})
			}
		}
		return
	}

	// Roz Packet (0x0CF1): สเตตัส / น้ำหนัก / Client Guard Check
	if pktID == 0x0CF1 {
		if len(packet) >= 13 {
			op := packet[12]
			payload := string(packet[13:])

			// Client Guard Check (OpCode 0x1E = 30 เท่านั้น ตาม Assembly ใน d3d9.dll)
			if op == 0x1E {
				cHex := extractKV(payload, "c")
				if cHex != "" {
					fmt.Printf("\n🛡️ [GUARD CHECK] ตรวจพบ Challenge จาก Map Server: c=%s (Payload: %q)\n", cHex, payload)
					respPkt := BuildGuardResponse(cHex)
					if err := b.sendMapPacket(respPkt); err != nil {
						fmt.Printf("❌ [GUARD CHECK] ส่งคำตอบ Guard ล้มเหลว: %v\n", err)
					} else {
						fmt.Printf("✨ [GUARD CHECK] ตอบคำถาม Client Guard (0x0CF0) ให้เซิร์ฟเวอร์สำเร็จเรียบร้อย!\n\n")
					}
					return
				}
			}

			// Op 10: สเตตัสเลเวล, อาชีพ, ชื่อตัวละคร
			if op == 10 {
				if name := extractKV(payload, "name"); name != "" {
					b.charName = name
				}
				if clsStr := extractKV(payload, "cls"); clsStr != "" {
					var cid int
					fmt.Sscanf(clsStr, "%d", &cid)
					b.jobClass = JobIDToName(uint16(cid))
				}
				if bl := extractKV(payload, "bl"); bl != "" {
					var newBl int
					fmt.Sscanf(bl, "%d", &newBl)
					if b.baseLv > 0 && newBl > b.baseLv {
						fmt.Printf("\n[🎉 Level Up!] Base Level เพิ่มขึ้น: %d -> %d\n", b.baseLv, newBl)
						b.baseLv = newBl
						b.checkAutoStat()
					} else {
						b.baseLv = newBl
					}
				}
				if jl := extractKV(payload, "jl"); jl != "" {
					var newJl int
					fmt.Sscanf(jl, "%d", &newJl)
					if b.jobLv > 0 && newJl > b.jobLv {
						fmt.Printf("\n[🎉 Level Up!] Job Level เพิ่มขึ้น: %d -> %d\n", b.jobLv, newJl)
						b.jobLv = newJl
						b.checkAutoSkill()
					} else {
						b.jobLv = newJl
					}
				}
				if m := extractKV(payload, "map"); m != "" {
					if NormalizeMapName(m) != NormalizeMapName(b.mapName) {
						b.currentField = nil
					}
					b.mapName = m
				}
				return
			}

			// Op 12: ค่าเลือด HP, มานา SP, น้ำหนัก Weight, เงิน Zeny
			if op == 12 {
				if hpStr := extractKV(payload, "hp"); hpStr != "" {
					fmt.Sscanf(hpStr, "%d", &b.hp)
				}
				if mhpStr := extractKV(payload, "mhp"); mhpStr != "" {
					fmt.Sscanf(mhpStr, "%d", &b.maxHp)
				}
				if spStr := extractKV(payload, "sp"); spStr != "" {
					fmt.Sscanf(spStr, "%d", &b.sp)
				}
				if mspStr := extractKV(payload, "msp"); mspStr != "" {
					fmt.Sscanf(mspStr, "%d", &b.maxSp)
				}
				if zStr := extractKV(payload, "z"); zStr != "" {
					fmt.Sscanf(zStr, "%d", &b.zeny)
				}
				if wStr := extractKV(payload, "w"); wStr != "" {
					fmt.Sscanf(wStr, "%d", &b.Weight)
				}
				if mwStr := extractKV(payload, "mw"); mwStr != "" {
					fmt.Sscanf(mwStr, "%d", &b.MaxWeight)
				}
				b.checkWeightAndSupplies()
				return
			}
		}
		return
	}
}

func extractKV(s, key string) string {
	target := key + "="
	startIdx := 0
	for {
		idx := strings.Index(s[startIdx:], target)
		if idx == -1 {
			return ""
		}
		realIdx := startIdx + idx
		if realIdx == 0 || s[realIdx-1] == '\n' || s[realIdx-1] == '\r' || s[realIdx-1] == '|' || s[realIdx-1] == ' ' || s[realIdx-1] == ';' {
			valStart := realIdx + len(target)
			end := strings.IndexAny(s[valStart:], "\r\n|")
			if end == -1 {
				return strings.TrimSpace(s[valStart:])
			}
			return strings.TrimSpace(s[valStart : valStart+end])
		}
		startIdx = realIdx + 1
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func JobIDToName(id uint16) string {
	switch id {
	case 0:
		return "Novice"
	case 1:
		return "Swordman"
	case 2:
		return "Mage"
	case 3:
		return "Archer"
	case 4:
		return "Acolyte"
	case 5:
		return "Merchant"
	case 6:
		return "Thief"
	case 7:
		return "Knight"
	case 8:
		return "Priest"
	case 9:
		return "Wizard"
	case 10:
		return "Blacksmith"
	case 11:
		return "Hunter"
	case 12:
		return "Assassin"
	case 14:
		return "Crusader"
	case 15:
		return "Monk"
	case 16:
		return "Sage"
	case 17:
		return "Rogue"
	case 18:
		return "Alchemist"
	case 19:
		return "Bard"
	case 20:
		return "Dancer"
	case 23:
		return "Super Novice"
	case 24:
		return "Gunslinger"
	case 25:
		return "Ninja"
	case 4001:
		return "High Novice"
	case 4008:
		return "Lord Knight"
	case 4009:
		return "High Priest"
	case 4010:
		return "High Wizard"
	case 4011:
		return "Whitesmith"
	case 4012:
		return "Sniper"
	case 4013:
		return "Assassin Cross"
	case 4015:
		return "Paladin"
	case 4016:
		return "Champion"
	default:
		if id > 0 {
			return fmt.Sprintf("Class #%d", id)
		}
		return "Novice"
	}
}
