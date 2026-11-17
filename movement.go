package main

import (
	"fmt"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"time"
)

func (b *SoloBot) checkAutoStat() {
	if b.cfg.AutoStat == "" {
		return
	}
	entries := strings.Split(b.cfg.AutoStat, ",")
	for _, entry := range entries {
		parts := strings.Fields(strings.TrimSpace(entry))
		if len(parts) >= 2 {
			name := strings.ToLower(parts[0])
			target, _ := strconv.Atoi(parts[1])
			var statID uint16
			switch name {
			case "str":
				statID = StatSTR
			case "agi":
				statID = StatAGI
			case "vit":
				statID = StatVIT
			case "int":
				statID = StatINT
			case "dex":
				statID = StatDEX
			case "luk":
				statID = StatLUK
			}
			if statID > 0 && target > 0 {
				fmt.Printf("[✨ Level Up] ทริกเกอร์อัป Status อัตโนมัติ: %s (ID: %d)\n", name, statID)
				_ = b.sendMapPacket(BuildAddStatus(statID))
				break
			}
		}
	}
}

func (b *SoloBot) checkAutoSkill() {
	if b.cfg.AutoSkill == "" {
		return
	}
	entries := strings.Split(b.cfg.AutoSkill, ",")
	for _, entry := range entries {
		parts := strings.Fields(strings.TrimSpace(entry))
		if len(parts) >= 1 {
			skid, _ := strconv.Atoi(parts[0])
			if skid > 0 {
				fmt.Printf("[✨ Level Up] ทริกเกอร์อัป Skill อัตโนมัติ: ID %d\n", skid)
				_ = b.sendMapPacket(BuildAddSkill(uint16(skid)))
				break
			}
		}
	}
}

func (b *SoloBot) checkWeightAndSupplies() {
	// ตรวจสอบตัวละครตาย (HP <= 0)
	if b.maxHp > 0 && b.hp <= 0 {
		if time.Since(b.LastDeathTime) >= 3*time.Second {
			b.LastDeathTime = time.Now()
			fmt.Println("\n[💀 Character Dead] ตัวละครเสียชีวิต (HP: 0)! ส่งคำสั่งฟื้นคืนชีพกลับจุดเซฟ (0x00B2)...")
			if b.AutoHuntActive {
				_ = b.StopAutoHunt()
			}
			_ = b.sendMapPacket(BuildRestart(0))
		}
		return
	}

	if b.MaxWeight <= 0 {
		return
	}
	weightPct := (b.Weight * 100) / b.MaxWeight
	if weightPct >= b.cfg.ReturnWeight && b.cfg.MerchantLoc != "" && !b.ReturningToMerchant {
		b.ReturningToMerchant = true
		fmt.Printf("\n[📦 Return to Merchant] น้ำหนักเต็ม %d%% (เกิน %d%%)! มุ่งหน้าไปหาแม่ค้า %s ที่ %s\n",
			weightPct, b.cfg.ReturnWeight, b.cfg.MerchantName, b.cfg.MerchantLoc)

		// สั่งปิด AutoHunt ทันทีเพื่อไม่ให้วิ่งตีมอนสเตอร์ขัดขวางการเดินทางกลับเมือง
		if b.AutoHuntActive {
			_ = b.StopAutoHunt()
		}

		// ใช้ Butterfly Wing ถ้ามี
		if b.Inventory != nil {
			bwingIdx, hasBwing := b.Inventory.FindItemIndex(602)
			if hasBwing {
				fmt.Println("[📦 Return to Merchant] กดใช้ Butterfly Wing วาร์ปกลับจุดเซฟ...")
				_ = b.sendMapPacket(BuildUseItem(bwingIdx))
			}
		}
		return
	}

	if weightPct < b.cfg.ReturnWeight && b.ReturningToMerchant {
		b.ReturningToMerchant = false
		fmt.Printf("\n[✨ Ready to Farm] น้ำหนักเบาแล้ว (%d%%)! เตรียมเดินทางกลับสู่ LockMap: %s\n", weightPct, b.cfg.LockMap)
	}
}

func (b *SoloBot) startGPSLoop(stopGPS chan struct{}) {
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-stopGPS:
			return
		case <-ticker.C:
			if !b.running || !b.cipherActive || b.mapName == "" || (b.trade != nil && b.trade.IsActive()) {
				continue
			}

			targetMap := b.cfg.LockMap
			var merchantTargetX, merchantTargetY int
			isVisitingMerchant := false

			if b.ReturningToMerchant && b.cfg.MerchantLoc != "" {
				mMap, mX, mY, ok := ParseLocation(b.cfg.MerchantLoc)
				if ok {
					targetMap = mMap
					merchantTargetX = mX
					merchantTargetY = mY
					isVisitingMerchant = true
				}
			}

			if targetMap == "" {
				continue
			}

			// หากอยู่นอกแมพเป้าหมาย: เดินข้ามประตูวาร์ป
			if NormalizeMapName(b.mapName) != NormalizeMapName(targetMap) {
				if b.currentX <= 0 || b.currentY <= 0 || b.router == nil {
					continue
				}

				if b.currentField == nil || b.currentField.Name != NormalizeMapName(b.mapName) {
					fld, err := LoadMapField(b.mapName)
					if err == nil {
						b.currentField = fld
						fmt.Printf("[🗺️ Field] โหลดข้อมูลตารางแผนที่ %s (%dx%d) สำเร็จ\n", fld.Name, fld.Width, fld.Height)
					}
				}

				route := b.router.FindRoute(b.mapName, targetMap)
				if len(route) == 0 {
					continue
				}

				nextP := route[0]
				dx := nextP.FromX - b.currentX
				dy := nextP.FromY - b.currentY
				dist := math.Hypot(float64(dx), float64(dy))

				if dist <= 3 {
					stepX := nextP.FromX
					stepY := nextP.FromY
					_ = b.sendMapPacket(BuildMove(stepX, stepY))
					b.currentX = stepX
					b.currentY = stepY
					time.Sleep(2 * time.Second)
					continue
				}

				var targetX, targetY int
				stepsWalked := 12
				if b.currentField != nil {
					path := b.currentField.FindPath(b.currentX, b.currentY, nextP.FromX, nextP.FromY, 20000)
					if len(path) > 0 {
						stepIdx := 12
						if stepIdx >= len(path) {
							stepIdx = len(path) - 1
						}
						targetX = path[stepIdx][0]
						targetY = path[stepIdx][1]
						stepsWalked = stepIdx
					}
				} else {
					ratio := 12.0 / dist
					targetX = b.currentX + int(math.Round(float64(dx)*ratio))
					targetY = b.currentY + int(math.Round(float64(dy)*ratio))
				}

				_ = b.sendMapPacket(BuildMove(targetX, targetY))
				walkDuration := time.Duration(stepsWalked*170) * time.Millisecond
				if walkDuration < 800*time.Millisecond {
					walkDuration = 800 * time.Millisecond
				}
				time.Sleep(walkDuration)
				b.currentX = targetX
				b.currentY = targetY
			} else {
				// อยู่ในแมพเป้าหมายแล้ว
				if isVisitingMerchant {
					dist := math.Hypot(float64(b.currentX-merchantTargetX), float64(b.currentY-merchantTargetY))
					if dist <= 1.5 {
						// ยืนหน้าแม่ค้าแล้ว!
						if b.trade != nil && !b.trade.IsActive() && b.cfg.MerchantName != "" {
							itemsToTrade := b.Inventory.GetItemsToTrade(b.trade.Config)
							if len(itemsToTrade) == 0 {
								fmt.Println("[✨ Town Cycle] ไม่มีไอเทมขยะที่ต้องส่งมอบแล้ว! เคลียร์โหมดกลับเมือง พร้อมกลับไปฟาร์มต่อ...")
								b.ReturningToMerchant = false
								time.Sleep(1 * time.Second)
								continue
							}

							if time.Since(b.lastGPSMove) >= 3*time.Second {
								b.lastGPSMove = time.Now()
								if b.trade.MerchantGID > 0 {
									fmt.Printf("\n[🤝 Deal Initiate] ยื่นคำขอดีลใส่แม่ค้า %s (GID: %d)...\n", b.cfg.MerchantName, b.trade.MerchantGID)
									b.trade.StartDealSession(b.trade.MerchantGID)
								} else {
									rnd := rand.Intn(9000) + 1000
									msg := fmt.Sprintf("deal %d", rnd)
									fmt.Printf("[💬 Whisper] ส่งข้อความหา %s: %s\n", b.cfg.MerchantName, msg)
									b.SendPM(b.cfg.MerchantName, msg)
								}
							}
						}
						time.Sleep(1 * time.Second)
						continue
					}

					// เดินเข้าไปหาพิกัดแม่ค้า
					if b.currentField == nil || b.currentField.Name != NormalizeMapName(b.mapName) {
						fld, err := LoadMapField(b.mapName)
						if err == nil {
							b.currentField = fld
						}
					}

					var walkX, walkY int
					stepsWalked := 10
					if b.currentField != nil {
						path := b.currentField.FindPath(b.currentX, b.currentY, merchantTargetX, merchantTargetY, 20000)
						if len(path) > 0 {
							stepIdx := 10
							if stepIdx >= len(path) {
								stepIdx = len(path) - 1
							}
							walkX = path[stepIdx][0]
							walkY = path[stepIdx][1]
							stepsWalked = stepIdx
						}
					}
					if walkX > 0 && walkY > 0 {
						_ = b.sendMapPacket(BuildMove(walkX, walkY))
						walkDuration := time.Duration(stepsWalked*170) * time.Millisecond
						if walkDuration < 800*time.Millisecond {
							walkDuration = 800 * time.Millisecond
						}
						time.Sleep(walkDuration)
						b.currentX = walkX
						b.currentY = walkY
					}
				} else {
					// อยู่ใน LockMap แล้ว!
					// ตรวจสอบชาแนล: ห้ามทำในเมืองเด็ดขาด (เช็กจาก Cities table)
					isCity := b.router != nil && b.router.IsCity(b.mapName)
					rawMap := strings.TrimSuffix(strings.TrimSpace(b.mapName), ".gat")
					isFieldOrDun := strings.Contains(rawMap, "_fild") || strings.Contains(rawMap, "_dun") || strings.Contains(rawMap, "fild") || strings.Contains(rawMap, "dun")
					needsChannelB := !isCity && isFieldOrDun && !strings.HasSuffix(rawMap, "b")

					if needsChannelB && b.ChannelNpcFound && b.ChannelNpcGID > 0 {
						if time.Since(b.LastChannelTry) >= 4*time.Second {
							b.LastChannelTry = time.Now()
							fmt.Printf("\n[📡 Channel Sync] แมพฟาร์ม '%s' ยังไม่ใช่ช่อง B! กำลังเริ่มคุยเครื่องย้ายช่อง (GID: %d)...\n", b.mapName, b.ChannelNpcGID)
							_ = b.sendMapPacket(BuildTalk(b.ChannelNpcGID))
						}
						time.Sleep(1 * time.Second)
						continue
					}

					// ถ้ายังไม่ได้เปิด AutoHunt ให้เปิดทันที
					if !b.AutoHuntActive && b.cfg.LockMap != "" {
						_ = b.TriggerAutoHunt()
					}
				}
			}
		}
	}
}

func ParseLocation(loc string) (string, int, int, bool) {
	clean := strings.ReplaceAll(loc, ",", " ")
	parts := strings.Fields(clean)
	if len(parts) >= 3 {
		m := NormalizeMapName(parts[0])
		x, errX := strconv.Atoi(parts[1])
		y, errY := strconv.Atoi(parts[2])
		if errX == nil && errY == nil {
			return m, x, y, true
		}
	}
	return "", 0, 0, false
}
