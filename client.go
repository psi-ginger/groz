package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	// DebugMovement สวิตช์ซ่อน log การก้าวเดินตามที่พี่แมวสั่ง (false = ไม่สแปมหน้าจอ)
	DebugMovement = false
)

type BotClient struct {
	Cfg Config

	AccountID  uint32
	SessionID1 uint32
	SessionID2 uint32
	Sex        byte

	CharID uint32

	CharPort int
	MapPort  int

	CurrentMap string
	LockMap    string
	CurrentX   int
	CurrentY   int
	InitBuffer []byte
	TargetGID  uint32

	// สถิติ EXP & เลเวล
	BaseLevel   int
	JobLevel    int
	BaseExp     uint64
	BaseExpNext uint64
	PrevBaseExp uint64
	BaseGain    uint64
	JobExp      uint64
	JobExpNext  uint64
	PrevJobExp  uint64
	JobGain     uint64

	// ค่าพลัง & น้ำหนัก
	HP        int
	MaxHP     int
	SP        int
	MaxSP     int
	Zeny      int
	Weight    int
	MaxWeight int

	ProfileID   string
	ProfileName string
	CharName    string
	JobClass    string
	JobID       uint16

	Inventory *InventoryManager
	Trade     *TradeManager

	Router        *PortalGraph
	CurrentField  *MapField
	PacketLengths map[uint16]int
	MapWs         *WsConn
	sendMu       sync.Mutex
	ClientCipher *Ec
	ServerCipher *Ec
	CipherActive bool
	Running        bool
	IsDead         bool
	AutoHuntActive bool
	LastGPSMove         time.Time
	ReturningToMerchant bool
	MerchantName        string
	MerchantLoc         string
	AutoStat            string
	AutoSkill           string
}

func NewBotClient(cfg Config) *BotClient {
	router, _ := LoadPortalGraph("portals.txt")
	pktLens := LoadPacketLengths("tables/recvpackets-v20250716.txt", "../openkore/tables/recvpackets-v20250716.txt")
	lockMap := "pay_dun00" // ค่าเริ่มต้นถ้ำพายอน
	c := &BotClient{
		Cfg:           cfg,
		LockMap:       lockMap,
		Router:        router,
		PacketLengths: pktLens,
	}
	c.Inventory = NewInventoryManager()
	c.Trade = NewTradeManager(c)
	return c
}

func NewBotClientFromProfile(p Profile) *BotClient {
	router, _ := LoadPortalGraph("portals.txt")
	pktLens := LoadPacketLengths("tables/recvpackets-v20250716.txt", "../openkore/tables/recvpackets-v20250716.txt")
	lockMap := p.LockMap
	if lockMap == "" {
		lockMap = "pay_dun00"
	}
	cfg := Config{
		Username: p.Username,
		Password: p.Password,
		CharSlot: p.CharSlot,
	}
	c := &BotClient{
		ProfileID:     p.ID,
		ProfileName:   p.Name,
		Cfg:           cfg,
		LockMap:       lockMap,
		Router:        router,
		PacketLengths: pktLens,
		MerchantName:  p.MerchantName,
		MerchantLoc:   p.MerchantLoc,
		AutoStat:      p.AutoStat,
		AutoSkill:     p.AutoSkill,
	}
	c.Inventory = NewInventoryManager()
	c.Trade = NewTradeManager(c)
	if c.Trade != nil {
		c.Trade.MerchantName = p.MerchantName
		c.Trade.MerchantGID = p.MerchantGID
		if c.Trade.MerchantGID == 0 && strings.EqualFold(p.MerchantName, "Mayu") {
			c.Trade.MerchantGID = 2900981
		}
	}
	return c
}

func (c *BotClient) SendPM(recipient, message string) {
	if c.MapWs != nil {
		c.sendMapPacket(BuildWhisper(recipient, message))
	}
}

func (c *BotClient) Stop() {
	c.Running = false
	if c.MapWs != nil {
		c.MapWs.Close()
	}
}

// StartLoginFlow รันขั้นตอน Login -> Char -> Map -> เปิดปุ่มบอท แบบ Native WSS 100%
func (c *BotClient) StartLoginFlow() error {
	fmt.Printf("[1/4] เชื่อมต่อ Login Server ตรงผ่าน WSS (พอร์ต 6900)...\n")
	if err := c.stepLogin(); err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	fmt.Printf("[2/4] เชื่อมต่อ Char Server ตรงผ่าน WSS (พอร์ต %d)...\n", c.CharPort)
	if err := c.stepChar(); err != nil {
		return fmt.Errorf("char server failed: %w", err)
	}

	fmt.Printf("[3/4] เชื่อมต่อ Map Server ตรงผ่าน WSS (พอร์ต %d)...\n", c.MapPort)
	if err := c.stepMap(); err != nil {
		return fmt.Errorf("map server failed: %w", err)
	}

	if NormalizeMapName(c.CurrentMap) == NormalizeMapName(c.LockMap) {
		fmt.Printf("[4/4] ยืนบน LockMap (%s) แล้ว! เตรียมเปิดระบบล่าอัตโนมัติ 0x0CF0 หลังเชื่อมต่อสำเร็จ...\n", c.LockMap)
	} else {
		fmt.Printf("[4/4] ยืนบนแมพ %s (เป้าหมาย: %s) -> เริ่มต้นระบบนำทาง GPS สู่แมพฟาร์ม...\n", c.CurrentMap, c.LockMap)
	}
	c.AutoHuntActive = false

	c.maintainMapConnection()
	return nil
}

func (c *BotClient) stepLogin() error {
	ws, err := DialWs(6900)
	if err != nil {
		return err
	}
	defer ws.Close()

	// 1. ส่ง Master Login (0x0064, Version 55)
	loginPkt := BuildMasterLogin(55, c.Cfg.Username, c.Cfg.Password, 0)
	if _, err := ws.Write(loginPkt); err != nil {
		return err
	}

	// 2. อ่าน Response จาก Login Server
	data, err := ws.Read()
	if err != nil {
		return err
	}

	if len(data) < 3 {
		return fmt.Errorf("login response too short (%d bytes)", len(data))
	}

	pktID := binary.LittleEndian.Uint16(data[0:2])
	fmt.Printf("      Login Server ตอบกลับ: 0x%04X (%d bytes, Hex: %s)\n", pktID, len(data), DumpHex(data))

	if pktID != 0x0AC4 && pktID != 0x0069 {
		var reason byte = 0
		if len(data) >= 3 {
			reason = data[2]
		}
		return fmt.Errorf("login failed with packet 0x%04X (reason: %d, hex: %s)", pktID, reason, DumpHex(data))
	}

	// ดึง AccountID และ SessionID
	if len(data) >= 47 {
		c.SessionID1 = binary.LittleEndian.Uint32(data[4:8])
		c.AccountID = binary.LittleEndian.Uint32(data[8:12])
		c.SessionID2 = binary.LittleEndian.Uint32(data[12:16])
		c.Sex = data[46]
	} else if len(data) >= 16 {
		c.AccountID = binary.LittleEndian.Uint32(data[2:6])
		c.SessionID1 = binary.LittleEndian.Uint32(data[6:10])
	}

	c.CharPort = 6121
	fmt.Printf("      เข้าสู่ระบบสำเร็จ! AccountID: %d, Sex: %d\n", c.AccountID, c.Sex)
	return nil
}

func (c *BotClient) stepChar() error {
	ws, err := DialWs(c.CharPort)
	if err != nil {
		return err
	}
	defer ws.Close()

	// 1. ส่ง Char Login (0x0065, 17 bytes)
	charLoginPkt := BuildCharLogin(c.AccountID, c.SessionID1, c.SessionID2, c.Sex)
	if _, err := ws.Write(charLoginPkt); err != nil {
		return err
	}

	// อ่านข้อความที่ 1: Account ID preamble (4 bytes)
	data, err := ws.Read()
	if err != nil {
		return err
	}

	// ถ้าข้อความแรกคือ 4 bytes ของ Account ID ให้อ่านข้อความที่ 2 เป็น Char List (0x082D)
	if len(data) == 4 {
		data, err = ws.Read()
		if err != nil {
			return err
		}
	}
	pktID := binary.LittleEndian.Uint16(data[0:2])
	fmt.Printf("      Char Server ตอบกลับ Char List: 0x%04X (ความยาว %d bytes)\n", pktID, len(data))

	// ถอดรหัส CharBlockSize (175 bytes) ของตัวละคร Slot ที่เลือก เพื่อดึงชื่อและเลเวลตั้งต้น
	if len(data) >= 4 {
		// ใน 0x082D บล็อกตัวละครจะเริ่มหลัง header (มักเป็น offset 8 หรือ 28 ขึ้นกับเวอร์ชัน)
		// หาตำแหน่ง CharID และสกัด BaseLv / JobLv
		for offset := 4; offset+175 <= len(data); offset += 175 {
			block := data[offset : offset+175]
			cid := binary.LittleEndian.Uint32(block[0:4])
			if cid > 0 {
				jobLv := binary.LittleEndian.Uint32(block[20:24])
				baseLv := binary.LittleEndian.Uint16(block[78:80])
				nameBytes := bytes.TrimRight(block[98:122], "\x00")
				c.BaseLevel = int(baseLv)
				c.JobLevel = int(jobLv)
				fmt.Printf("      [Slot %d] ชื่อ: %s | Lv.%d/%d | CharID: %d\n", c.Cfg.CharSlot, string(nameBytes), c.BaseLevel, c.JobLevel, cid)
				break
			}
		}
	}

	// 2. ส่ง Char Select (0x0066) เพื่อเลือกตัวละครตาม Slot
	selectPkt := BuildCharSelect(c.Cfg.CharSlot)
	if _, err := ws.Write(selectPkt); err != nil {
		return err
	}

	// 3. อ่าน Map Info (0x0071 - HC_NOTIFY_ZONESVR)
	data, err = ws.Read()
	if err != nil {
		return err
	}

	if len(data) >= 4 {
		respID := binary.LittleEndian.Uint16(data[0:2])
		fmt.Printf("      Char Select ตอบกลับ: 0x%04X (ความยาว %d bytes)\n", respID, len(data))

		if (respID == 0x0071 || respID == 0x0AC5) && len(data) >= 28 {
			c.CharID = binary.LittleEndian.Uint32(data[2:6])
			mapName := string(bytes.TrimRight(data[6:22], "\x00"))
			port := binary.LittleEndian.Uint16(data[26:28])
			c.MapPort = int(port)
			c.CurrentMap = mapName
			var serverAddr string
			if respID == 0x0AC5 && len(data) >= 156 {
				serverAddr = string(bytes.TrimRight(data[28:156], "\x00"))
				if strings.Contains(serverAddr, ":") {
					parts := strings.Split(serverAddr, ":")
					if len(parts) >= 2 {
						var p int
						fmt.Sscanf(parts[1], "%d", &p)
						if p > 0 {
							c.MapPort = p
						}
					}
				}
			}
			fmt.Printf("      [OK] ได้พิกัดแมพ: %s | Map Server พอร์ต: %d | CharID: %d | ServerAddr: %q\n", mapName, c.MapPort, c.CharID, serverAddr)
		}
	}

	if c.MapPort == 0 {
		c.MapPort = 5121
	}
	return nil
}

func (c *BotClient) stepMap() error {
	c.CipherActive = false
	c.InitBuffer = nil

	ws, err := DialWs(c.MapPort)
	if err != nil {
		return err
	}
	c.MapWs = ws

	// 1. ส่ง Map Login (0x0436, 19 bytes) แบบ Plaintext ตรงเข้า WSS
	mapLoginPkt := BuildMapLogin(c.AccountID, c.CharID, c.SessionID1, c.Sex)
	if len(mapLoginPkt) >= 19 {
		key17 := mapLoginPkt[2:19]
		c.ClientCipher = NewEc('C', key17)
		c.ServerCipher = NewEc('S', key17)
	}

	if _, err := ws.Write(mapLoginPkt); err != nil {
		return err
	}
	fmt.Printf("      ส่ง Map Login (0x0436) เรียบร้อย รอรับพิกัดยืนยันเข้าแมพ...\n")

	return nil
}

func (c *BotClient) sendMapPacket(pkt []byte) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()

	out := pkt
	if c.CipherActive && c.ClientCipher != nil {
		out = make([]byte, len(pkt))
		copy(out, pkt)
		c.ClientCipher.Xor(out)
	}

	_, err := c.MapWs.Write(out)
	return err
}

func (c *BotClient) triggerAutoHunt() error {
	// 1. ส่ง OpPing (OpCode 2) ขอสถานะและ Config ของบอทจากเซิร์ฟเวอร์ก่อน
	pingPkt := BuildRozPacket(OpPing, "")
	c.sendMapPacket(pingPkt)

	// รอสักครู่แล้วส่งคำสั่งเปิดระบบล่าอัตโนมัติ (OpCode 3, "full")
	time.Sleep(300 * time.Millisecond)
	huntPkt := BuildRozPacket(OpStart, "full")
	if err := c.sendMapPacket(huntPkt); err != nil {
		return err
	}
	fmt.Printf("      [SUCCESS] ยิงคำสั่ง [ล่าอัตโนมัติ 0x0CF0] (ใช้ Config ในเกมของพี่แมว) เรียบร้อย!\n")
	return nil
}

func (c *BotClient) checkAutoStatAndSkill() {
	if c.AutoStat == "" && c.AutoSkill == "" {
		return
	}

	// 1. Auto Status (รูปแบบเช่น "int 99, dex 80, vit 30")
	if c.AutoStat != "" {
		entries := strings.Split(c.AutoStat, ",")
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
					c.sendMapPacket(BuildAddStatus(statID))
					break
				}
			}
		}
	}

	// 2. Auto Skill (รูปแบบเช่น "1 9, 19 10, 20 10" หรือ "19, 20")
	if c.AutoSkill != "" {
		entries := strings.Split(c.AutoSkill, ",")
		for _, entry := range entries {
			parts := strings.Fields(strings.TrimSpace(entry))
			if len(parts) >= 1 {
				skid, _ := strconv.Atoi(parts[0])
				if skid > 0 {
					c.sendMapPacket(BuildAddSkill(uint16(skid)))
				}
			}
		}
	}
}

// ParseLocation แปลงข้อความพิกัด เช่น "payon 167 104" หรือ "payon 167, 104" เป็นชื่อแมพและพิกัด X, Y
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

// checkWeightAndSupplies ตรวจสอบน้ำหนักตัวละครและสลับโหมดกลับเมืองไปหาแม่ค้า
func (c *BotClient) checkWeightAndSupplies() {
	if c.MaxWeight <= 0 {
		return
	}
	weightPct := (c.Weight * 100) / c.MaxWeight

	// 1. ถ้าน้ำหนัก >= 50% และตั้งค่าจุดนัดพบแม่ค้าไว้ -> เริ่มกลับไปหาแม่ค้า
	if weightPct >= 50 && c.MerchantLoc != "" && !c.ReturningToMerchant {
		c.ReturningToMerchant = true
		fmt.Printf("\n[📦 Return to Merchant] น้ำหนักเต็ม %d%% (เกิน 50%%)! สั่งหยุดฟาร์ม และมุ่งหน้าไปหาแม่ค้า %s ที่ %s\n",
			weightPct, c.MerchantName, c.MerchantLoc)

		// สั่งปิด AutoHunt ในเกมทันที
		if c.AutoHuntActive {
			c.AutoHuntActive = false
			_ = c.sendMapPacket(BuildRozPacket(OpStop, ""))
		}

		// ใช้ Butterfly Wing ถ้ามีในกระเป๋า
		if c.Inventory != nil {
			bwingIdx, hasBwing := c.Inventory.FindItemIndex(602)
			if hasBwing {
				fmt.Println("[📦 Return to Merchant] กดใช้ Butterfly Wing วาร์ปกลับจุดเซฟ...")
				_ = c.sendMapPacket(BuildUseItem(bwingIdx))
			}
		}
		return
	}

	// 2. ถ้าน้ำหนักลดลงมาแล้ว (< 50%) และกำลังอยู่ในโหมดไปหาแม่ค้า -> เคลียร์สถานะ พร้อมกลับไปฟาร์มต่อ
	if weightPct < 50 && c.ReturningToMerchant {
		c.ReturningToMerchant = false
		fmt.Printf("\n[✨ Ready to Farm] น้ำหนักเบาแล้ว (%d%%)! เตรียมเดินทางกลับสู่ LockMap: %s\n", weightPct, c.LockMap)
	}
}

func (c *BotClient) maintainMapConnection() {
	c.Running = true
	defer func() { c.Running = false }()

	// Heartbeat Goroutine: เลี้ยงสายด้วย RO Sync (0x0360) + Rozclub Ping (0x0CF0)
	go func() {
		ticker := time.NewTicker(2500 * time.Millisecond)
		defer ticker.Stop()

		for c.Running {
			<-ticker.C
			if c.MapWs != nil {
				// บน Field Map ต้องรอให้ Stream Cipher Handshake เสร็จสมบูรณ์ก่อนส่ง Heartbeat
				if c.MapPort != 5121 && !c.CipherActive {
					continue
				}

				// 1. RO Standard Keep-Alive Sync (0x0360) ป้องกัน Map Server ตัดสาย
				syncPkt := BuildSync(uint32(time.Now().UnixMilli()))
				c.sendMapPacket(syncPkt)

				// 2. Rozclub Auto-Bot Ping (0x0CF0, Op 2) เฉพาะ Map Server ฟิลด์ล่ามอน (ไม่ใช่เมือง พอร์ต 5121)
				if c.MapPort != 5121 {
					pingPkt := BuildRozPacket(OpPing, "")
					c.sendMapPacket(pingPkt)
				}

				// 3. ตรวจสอบอัพสเตตัสและสกิลอัตโนมัติตามที่พี่แมวตั้งค่า
				c.checkAutoStatAndSkill()

				// 4. ตรวจสอบน้ำหนักตัวละครและภารกิจกลับไปหาแม่ค้า
				c.checkWeightAndSupplies()
			}
		}
	}()

	// GPS Travel Loop: ตรวจสอบและนำทางต่อเนื่องตามความเร็วเดินจริง
	go func() {
		for c.Running {
			time.Sleep(300 * time.Millisecond)

			// ข้ามถ้ากำลังดีลของ หรือข้อมูลยังไม่พร้อม หรือยังรอ Cipher Handshake อยู่บน Field Map
			if (c.Trade != nil && c.Trade.IsActive()) || c.CurrentMap == "" || c.LockMap == "" || c.Router == nil || (c.MapPort != 5121 && !c.CipherActive) {
				continue
			}

			// 1. ระบุเป้าหมายแมพ (ถ้าอยู่ในโหมดกลับไปหาแม่ค้า ให้มุ่งหน้าสู่แมพของแม่ค้า)
			targetMap := c.LockMap
			var merchantTargetX, merchantTargetY int
			isVisitingMerchant := false

			if c.ReturningToMerchant && c.MerchantLoc != "" {
				mMap, mX, mY, ok := ParseLocation(c.MerchantLoc)
				if ok {
					targetMap = mMap
					merchantTargetX = mX
					merchantTargetY = mY
					isVisitingMerchant = true
				}
			}

			if NormalizeMapName(c.CurrentMap) != NormalizeMapName(targetMap) {
				// 1. กำลังเดินทางข้ามแมพ: สั่งปิด AutoHunt ในเกมชั่วคราวเพื่อไม่ให้แย่งเดิน/นั่ง
				if c.AutoHuntActive && c.MapPort != 5121 {
					c.AutoHuntActive = false
					c.sendMapPacket(BuildRozPacket(OpStop, ""))
					fmt.Printf("\n[🧭 GPS] กำลังเดินทางข้ามแมพ -> ปิดระบบล่าอัตโนมัติชั่วคราว\n")
				}

				// 2. ถ้ายังไม่ทราบพิกัดจริง ให้รอรับพิกัดจากเซิร์ฟเวอร์ก่อน (ห้ามเดินจาก 0, 0 เด็ดขาด)
				if c.CurrentX <= 0 || c.CurrentY <= 0 {
					if c.MapPort != 5121 {
						_ = c.sendMapPacket(BuildRozPacket(OpPing, ""))
					}
					time.Sleep(500 * time.Millisecond)
					continue
				}

				// โหลดตาราง Grid แผนที่หากยังไม่ได้โหลด
				if c.CurrentField == nil || c.CurrentField.Name != NormalizeMapName(c.CurrentMap) {
					fld, err := LoadMapField(c.CurrentMap)
					if err == nil {
						c.CurrentField = fld
						fmt.Printf("[🗺️ Field] โหลดข้อมูลตารางแผนที่ %s (%dx%d) สำเร็จ\n", fld.Name, fld.Width, fld.Height)
					}
				}

				// 3. คำนวณเส้นทางสู่ targetMap
				route := c.Router.FindRoute(c.CurrentMap, targetMap)
				if len(route) == 0 {
					time.Sleep(1 * time.Second)
					continue
				}

				nextP := route[0]

				// ตรวจสอบประตูวาร์ปทั้งหมดที่นำไปสู่ ToMap เพื่อเลือกประตูที่เดินถึงได้จริงจากพิกัดปัจจุบัน
				candidatePortals := c.Router.GetPortalsTo(c.CurrentMap, nextP.ToMap)
				if len(candidatePortals) > 1 && c.CurrentField != nil {
					bestPortal := nextP
					bestPathLen := 999999
					foundReachable := false
					for _, cand := range candidatePortals {
						p := c.CurrentField.FindPath(c.CurrentX, c.CurrentY, cand.FromX, cand.FromY, 20000)
						if len(p) > 0 {
							lastNode := p[len(p)-1]
							if math.Hypot(float64(lastNode[0]-cand.FromX), float64(lastNode[1]-cand.FromY)) <= 5 {
								if len(p) < bestPathLen {
									bestPathLen = len(p)
									bestPortal = cand
									foundReachable = true
								}
							}
						}
					}
					if foundReachable {
						nextP = bestPortal
					}
				}

				dx := nextP.FromX - c.CurrentX
				dy := nextP.FromY - c.CurrentY
				dist := math.Hypot(float64(dx), float64(dy))

				// ถ้ายืนชิดวาร์ปแล้ว (ห่าง <= 3 ช่อง) ก้าวเหยียบวาร์ปตรงๆ
				if dist <= 3 {
					stepX := nextP.FromX
					stepY := nextP.FromY

					// ถ้าตัวละครยืนทับหรือชิดพิกัดวาร์ปมากแล้ว (dist <= 1.5) ให้ก้าวข้ามเส้น Trigger ของวาร์ปไปอีก 1-2 ช่อง
					if dist <= 1.5 {
						if c.CurrentField != nil {
							if stepY <= 25 {
								stepY -= 2
							} else if stepY >= c.CurrentField.Height-25 {
								stepY += 2
							} else if stepX <= 25 {
								stepX -= 2
							} else if stepX >= c.CurrentField.Width-25 {
								stepX += 2
							} else {
								// วาร์ปในเมือง/ดันเจี้ยน: ก้าวไปข้างหน้าอีก 1 ช่องตามทิศทางเดิม
								if dy < 0 {
									stepY--
								} else if dy > 0 {
									stepY++
								}
								if dx < 0 {
									stepX--
								} else if dx > 0 {
									stepX++
								}
							}
						} else {
							if stepY <= 25 {
								stepY -= 2
							} else if stepY >= 350 {
								stepY += 2
							}
						}
					}

					if DebugMovement {
						fmt.Printf("\n[🧭 GPS] ถึงระยะวาร์ปแล้ว (ห่าง %.0f ช่อง)! ก้าวเหยียบวาร์ปมุ่งหน้า %s ที่ (%d, %d)\n", dist, nextP.ToMap, stepX, stepY)
					}
					_ = c.sendMapPacket(BuildMove(stepX, stepY))
					time.Sleep(2 * time.Second)
					continue
				}

				// ค้นหาเส้นทาง A* จริงบนตารางแมพเพื่อเลี่ยงต้นไม้/กำแพง
				var targetX, targetY int
				stepsWalked := 12
				if c.CurrentField != nil {
					path := c.CurrentField.FindPath(c.CurrentX, c.CurrentY, nextP.FromX, nextP.FromY, 20000)
					if len(path) > 0 {
						stepIdx := 12
						if stepIdx >= len(path) {
							stepIdx = len(path) - 1
						}
						// ทอนก้าวถอยหลังลงมาจนกว่าจะมี Line of Sight (LOS) เส้นตรงที่ไม่มีมุมตึก/กำแพงขวาง
						for stepIdx > 0 && !c.CurrentField.HasLOS(c.CurrentX, c.CurrentY, path[stepIdx][0], path[stepIdx][1]) {
							stepIdx--
						}
						targetX = path[stepIdx][0]
						targetY = path[stepIdx][1]
						stepsWalked = stepIdx + 1
						if DebugMovement {
							fmt.Printf("\n[🧭 GPS] นำทางสู่ %s: ก้าวเดิน A* %d ช่องจาก (%d, %d) ไปที่ (%d, %d) [เส้นทางคงเหลือ: %d ช่อง]\n",
								nextP.ToMap, stepsWalked, c.CurrentX, c.CurrentY, targetX, targetY, len(path))
						}
					} else {
						// หากเดินตรงไม่ได้และ A* ไม่พบ ให้ลองหาช่องเดินได้ใกล้ๆ
						ratio := 12.0 / dist
						candX := c.CurrentX + int(math.Round(float64(dx)*ratio))
						candY := c.CurrentY + int(math.Round(float64(dy)*ratio))
						targetX, targetY = c.CurrentField.FindNearestWalkable(candX, candY, 5)
						if targetX == c.CurrentX && targetY == c.CurrentY {
							if DebugMovement {
								fmt.Printf("\n[⚠️ GPS] ติดสิ่งกีดขวางไม่สามารถเดินต่อไปยัง %s ได้จาก (%d, %d)\n", nextP.ToMap, c.CurrentX, c.CurrentY)
							}
							time.Sleep(1 * time.Second)
							continue
						}
						if DebugMovement {
							fmt.Printf("\n[🧭 GPS] นำทางสู่ %s: เลี่ยงสิ่งกีดขวางก้าวไปที่ (%d, %d)\n", nextP.ToMap, targetX, targetY)
						}
					}
				} else {
					ratio := 12.0 / dist
					targetX = c.CurrentX + int(math.Round(float64(dx)*ratio))
					targetY = c.CurrentY + int(math.Round(float64(dy)*ratio))
				}

				movePkt := BuildMove(targetX, targetY)
				_ = c.sendMapPacket(movePkt)

				// หน่วงเวลารอให้ตัวละครเดินถึงปลายทางจริง (ความเร็วเดิน RO ~160ms/ช่อง)
				walkDuration := time.Duration(stepsWalked*170) * time.Millisecond
				if walkDuration < 800*time.Millisecond {
					walkDuration = 800 * time.Millisecond
				}
				time.Sleep(walkDuration)

				// อัปเดตพิกัดตามก้าวเดินที่เดินสำเร็จแล้ว เพื่อให้ก้าวต่อไปคำนวณเส้นทางต่อได้ทันที
				c.CurrentX = targetX
				c.CurrentY = targetY
			} else {
				// ถึงแมพเป้าหมายแล้ว!
				if isVisitingMerchant {
					// 1. กำลังมาหาแม่ค้า: เดิน A* ต่อไปยังพิกัดเป้าหมาย (merchantTargetX, merchantTargetY)
					dist := math.Hypot(float64(c.CurrentX-merchantTargetX), float64(c.CurrentY-merchantTargetY))
					if dist <= 1.5 {
						// ถึงระยะยืนประชิดตัวแม่ค้าแล้ว (ห่าง <= 1 ช่อง เพื่อให้อยู่ใน TRADE_DISTANCE แน่นอน)!
						if DebugMovement {
							fmt.Printf("\n[🤝 Merchant] ยืนอยู่หน้าแม่ค้า %s แล้ว ที่ (%d, %d)\n", c.MerchantName, c.CurrentX, c.CurrentY)
						}

						// ยื่นดีลใส่แม่ค้าทันที (Initiate Deal)
						if c.Trade != nil && !c.Trade.IsActive() && c.MerchantName != "" {
							if time.Since(c.LastGPSMove) >= 3*time.Second {
								c.LastGPSMove = time.Now()
								if c.Trade.MerchantGID > 0 {
									fmt.Printf("\n[🤝 Deal Initiate] ยื่นคำขอดีลใส่แม่ค้า %s (GID: %d)...\n", c.MerchantName, c.Trade.MerchantGID)
									c.Trade.StartDealSession(c.Trade.MerchantGID)
								} else {
									// หากยังไม่ทราบ GID ให้ส่ง PM เรียกเบาๆ
									rnd := rand.Intn(9000) + 1000
									msg := fmt.Sprintf("deal %d", rnd)
									fmt.Printf("[💬 Whisper] ยืนข้างแม่ค้าแต่ยังไม่พบ GID -> ส่งข้อความหา %s: %s\n", c.MerchantName, msg)
									c.SendPM(c.MerchantName, msg)
								}
							}
						}
						time.Sleep(1 * time.Second)
						continue
					}

					// โหลดตาราง Grid แผนที่หากยังไม่ได้โหลด
					if c.CurrentField == nil || c.CurrentField.Name != NormalizeMapName(c.CurrentMap) {
						fld, err := LoadMapField(c.CurrentMap)
						if err == nil {
							c.CurrentField = fld
						}
					}

					// ค้นหาเส้นทาง A* เดินไปยังพิกัดแม่ค้า
					var walkX, walkY int
					stepsWalked := 10
					if c.CurrentField != nil {
						path := c.CurrentField.FindPath(c.CurrentX, c.CurrentY, merchantTargetX, merchantTargetY, 20000)
						if len(path) > 0 {
							stepIdx := 10
							if stepIdx >= len(path) {
								stepIdx = len(path) - 1
							}
							for stepIdx > 0 && !c.CurrentField.HasLOS(c.CurrentX, c.CurrentY, path[stepIdx][0], path[stepIdx][1]) {
								stepIdx--
							}
							walkX = path[stepIdx][0]
							walkY = path[stepIdx][1]
							stepsWalked = stepIdx + 1
						} else {
							walkX, walkY = c.CurrentField.FindNearestWalkable(merchantTargetX, merchantTargetY, 5)
						}
					} else {
						walkX = merchantTargetX
						walkY = merchantTargetY
					}

					if walkX > 0 && walkY > 0 {
						_ = c.sendMapPacket(BuildMove(walkX, walkY))
						walkDuration := time.Duration(stepsWalked*170) * time.Millisecond
						if walkDuration < 800*time.Millisecond {
							walkDuration = 800 * time.Millisecond
						}
						time.Sleep(walkDuration)
						c.CurrentX = walkX
						c.CurrentY = walkY
					}
					continue
				}

				// 2. ถ้าเป็น LockMap ปกติ: เปิด AutoHunt
				if !c.AutoHuntActive {
					if c.MapPort != 5121 && !c.CipherActive {
						time.Sleep(500 * time.Millisecond)
						continue
					}
					c.AutoHuntActive = true
					_ = c.triggerAutoHunt()
					fmt.Printf("\n[⚔️ LockMap] ยืนบนแมพเป้าหมาย %s แล้ว! เปิดระบบล่าอัตโนมัติ 0x0CF0 เรียบร้อย\n", c.LockMap)
				}
			}
		}
	}()

	// Read Loop
	for c.Running {
		var data []byte
		if len(c.InitBuffer) > 0 {
			data = c.InitBuffer
			c.InitBuffer = nil
		} else {
			var err error
			data, err = c.MapWs.Read()
			if err != nil {
				fmt.Printf("\n[!] ขาดการเชื่อมต่อกับ Map Server: %v\n", err)
				break
			}
		}

		if c.CipherActive && c.ServerCipher != nil {
			c.ServerCipher.Xor(data)
		} else if !c.CipherActive && len(data) >= 13 {
			pktID := binary.LittleEndian.Uint16(data[0:2])
			if pktID == 0x0CF1 && string(data[4:12]) == "THAA#9!Z" && data[12] == 40 {
				c.CipherActive = true
				fmt.Printf("\n[CIPHER ACTIVATED] ตรวจพบ Handshake 0x0CF1 จาก Map Server %d! เปิดสตรีมไซเฟอร์ปลอดภัยเรียบร้อย\n", c.MapPort)
				pktLen := int(binary.LittleEndian.Uint16(data[2:4]))
				if len(data) > pktLen {
					remaining := data[pktLen:]
					if c.ServerCipher != nil {
						c.ServerCipher.Xor(remaining)
					}
					data = remaining
				} else {
					continue
				}
			}
		}

		// แกะทุกแพ็กเก็ตที่มาใน Buffer เพื่อไม่ให้ตกหล่นข้อมูล เลเวล / EXP / HP
		for offset := 0; offset+2 <= len(data); {
			pktID := binary.LittleEndian.Uint16(data[offset : offset+2])
			if pktID == PacketPingLiveServer {
				_ = c.sendMapPacket(BuildPingLive())
				offset += 2
				continue
			}
			if pktID == 0x0073 && offset+11 <= len(data) {
				// ZC_PAR_CHANGE / map_loaded (11 bytes: Header 2B + Tick 4B + PosData 3B + Size 2B)
				b0 := uint16(data[offset+6])
				b1 := uint16(data[offset+7])
				b2 := uint16(data[offset+8])
				x := int((b0 << 2) | (b1 >> 6))
				y := int(((b1 & 0x3F) << 4) | (b2 >> 4))
				if x > 0 && y > 0 && x < 1024 && y < 1024 {
					c.CurrentX = x
					c.CurrentY = y
					fmt.Printf("\n[📍 พิกัดเกิด 0x0073] ยืนอยู่ที่ (%d, %d)\n", c.CurrentX, c.CurrentY)
				}
				offset += 11
				continue
			}
			if pktID == 0x02EB && offset+13 <= len(data) {
				// ZC_ACCEPT_ENTER2: 13 bytes
				b0 := uint16(data[offset+6])
				b1 := uint16(data[offset+7])
				b2 := uint16(data[offset+8])
				x := int((b0 << 2) | (b1 >> 6))
				y := int(((b1 & 0x3F) << 4) | (b2 >> 4))
				if x > 0 && y > 0 {
					c.CurrentX = x
					c.CurrentY = y
					fmt.Printf("\n[📍 พิกัดเกิด 0x02EB] ยืนอยู่ที่ (%d, %d)\n", c.CurrentX, c.CurrentY)

					// ตอบกลับด้วย MapLoaded (0x007D) และ BlockingPlayCancel (0x0447) หลังได้รับพิกัดยืนยันเข้าแมพแล้ว
					_ = c.sendMapPacket(BuildMapLoaded())
					_ = c.sendMapPacket(BuildBlockingPlayCancel())
				}
				offset += 13
				continue
			}

			// --- [👀 Auto-Discovery: Actor Spawn & Merchant Detection] ---
			if (pktID == 0x09FF || pktID == 0x0857 || pktID == 0x09DD || pktID == 0x0915) && offset+4 <= len(data) {
				pLen := int(binary.LittleEndian.Uint16(data[offset+2 : offset+4]))
				if pLen < 4 || offset+pLen > len(data) {
					break
				}
				if pLen >= 9 {
					actorID := binary.LittleEndian.Uint32(data[offset+5 : offset+9])
					if c.MerchantName != "" {
						targetNames := strings.Split(c.MerchantName, ",")
						chunk := string(data[offset+5 : offset+pLen])
						for _, mName := range targetNames {
							trimmed := strings.TrimSpace(mName)
							if trimmed != "" && strings.Contains(chunk, trimmed) {
								if c.Trade != nil && c.Trade.MerchantGID != actorID {
									c.Trade.MerchantGID = actorID
									c.Trade.MerchantName = trimmed
									fmt.Printf("\n[👀 Auto-Discovery] สแกนพบแม่ค้า '%s' ในจอ! บันทึกรหัสประจำตัว (GID: %d) ทันที\n", trimmed, actorID)
								}
								break
							}
						}
					}
				}
				offset += pLen
				continue
			}

			// --- [🤝 Deal / Exchange Packets] ---
			if (pktID == 0x00E5 && offset+26 <= len(data)) || (pktID == 0x01F4 && offset+32 <= len(data)) {
				// 0x00E5 (26 bytes) หรือ 0x01F4 (32 bytes): มีคนขอดีลมา!
				pLen := 26
				if pktID == 0x01F4 {
					pLen = 32
				}
				senderName := string(bytes.Trim(data[offset+2:offset+26], "\x00"))
				fmt.Printf("\n[🤝 Deal Request (0x%04X)] ตัวละคร '%s' ส่งคำขอดีลมา! กำลังกดตอบตกลง (0x00E6 3)...\n", pktID, senderName)
				_ = c.sendMapPacket(BuildDealReply(3)) // กดตอบตกลงทันที (Action 3 = Accept Deal)
				if c.Trade != nil {
					c.Trade.DealActive = true
					c.Trade.State = TradeDealWaitingHandshake
					if pktID == 0x01F4 {
						c.Trade.MerchantGID = binary.LittleEndian.Uint32(data[offset+26 : offset+30])
					}
				}
				offset += pLen
				continue
			}
			if (pktID == 0x00E7 && offset+3 <= len(data)) || (pktID == 0x01F5 && offset+9 <= len(data)) {
				// 0x00E7 (3 bytes) หรือ 0x01F5 (9 bytes): หน้าต่างดีลเปิดเรียบร้อย
				pLen := 3
				if pktID == 0x01F5 {
					pLen = 9
				}
				res := data[offset+2]
				fmt.Printf("\n[🤝 Deal Begin (0x%04X)] หน้าต่างดีลเปิดแล้ว (Status: %d)!\n", pktID, res)
				if (res == 0 || res == 3) && c.Trade != nil {
					c.Trade.HandleDealBegin()
				}
				offset += pLen
				continue
			}
			if pktID == 0x00EC && offset+3 <= len(data) {
				// ZC_CONCLUDE_EXCHANGE (3 bytes): อีกฝ่ายกด Lock ดีลแล้ว
				if c.Trade != nil {
					c.Trade.HandleDealOtherLocked()
				}
				offset += 3
				continue
			}
			if pktID == 0x00F0 && offset+3 <= len(data) {
				// ZC_EXEC_EXCHANGE (3 bytes): ดีลสำเร็จสมบูรณ์!
				res := data[offset+2]
				if res == 0 && c.Trade != nil {
					c.Trade.HandleDealComplete()
				}
				offset += 3
				continue
			}
			if pktID == 0x00EE && offset+2 <= len(data) {
				// ZC_CANCEL_EXCHANGE (2 bytes): ดีลถูกยกเลิก
				if c.Trade != nil {
					c.Trade.HandleDealCancel()
				}
				offset += 2
				continue
			}

			// --- [🎒 Inventory Packets] ---
			if pktID == 0x00AF && offset+6 <= len(data) {
				// ZC_ITEM_FALL_ENTRY / inventory_item_removed (6 bytes): idx uint16, amount uint16
				idx := binary.LittleEndian.Uint16(data[offset+2 : offset+4])
				amount := int(binary.LittleEndian.Uint16(data[offset+4 : offset+6]))
				if c.Inventory != nil {
					c.Inventory.RemoveItem(idx, amount)
				}
				offset += 6
				continue
			}

			// --- [🎒 Inventory Packets] ---
			if (pktID == 0x01EE || pktID == 0x02E8 || pktID == 0x0991) && offset+4 <= len(data) {
				// ข้อมูลไอเทมทั้งหมดในกระเป๋า (0x0991 ขนาด 24B, 0x02E8 ขนาด 22B, 0x01EE ขนาด 18B)
				pLen := int(binary.LittleEndian.Uint16(data[offset+2 : offset+4]))
				if offset+pLen <= len(data) {
					itemBytes := data[offset+4 : offset+pLen]
					itemSize := 18
					if pktID == 0x02E8 {
						itemSize = 22
					} else if pktID == 0x0991 {
						itemSize = 24
					}
					itemCount := 0
					for i := 0; i+itemSize <= len(itemBytes); i += itemSize {
						idx := binary.LittleEndian.Uint16(itemBytes[i : i+2])
						nameID := binary.LittleEndian.Uint16(itemBytes[i+2 : i+4])
						itemType := itemBytes[i+4]
						var amount int
						if pktID == 0x0991 {
							// 0x0991: ID(2), nameID(2), type(1), amount(2)
							amount = int(binary.LittleEndian.Uint16(itemBytes[i+5 : i+7]))
						} else {
							// 0x01EE / 0x02E8: ID(2), nameID(2), type(1), identified(1), amount(2)
							amount = int(binary.LittleEndian.Uint16(itemBytes[i+6 : i+8]))
						}
						if c.Inventory != nil {
							c.Inventory.SetItem(&Item{
								Index:      idx,
								NameID:     nameID,
								Amount:     amount,
								Type:       itemType,
								Identified: true,
							})
							itemCount++
						}
					}
					if itemCount > 0 {
						fmt.Printf("[🎒 Inventory (0x%04X)] ซิงค์ข้อมูลไอเทมในกระเป๋า %d ชิ้นเรียบร้อย\n", pktID, itemCount)
					}
					offset += pLen
					continue
				}
			}
			if (pktID == 0x02D4 && offset+39 <= len(data)) || (pktID == 0x00A0 && offset+33 <= len(data)) {
				// ไอเทมที่เก็บได้จากการฟาร์ม (0x02D4: 39 bytes, 0x00A0: 33 bytes)
				pLen := 33
				if pktID == 0x02D4 {
					pLen = 39
				}
				idx := binary.LittleEndian.Uint16(data[offset+2 : offset+4])
				amount := int(binary.LittleEndian.Uint16(data[offset+4 : offset+6]))
				nameID := binary.LittleEndian.Uint16(data[offset+6 : offset+8])
				itemType := data[offset+21]
				if c.Inventory != nil {
					// ถ้ามีอยู่แล้วให้บวกจำนวนเพิ่ม
					c.Inventory.mu.Lock()
					if it, exists := c.Inventory.Items[idx]; exists {
						it.Amount += amount
					} else {
						c.Inventory.Items[idx] = &Item{
							Index:      idx,
							NameID:     nameID,
							Amount:     amount,
							Type:       itemType,
							Identified: true,
						}
					}
					c.Inventory.mu.Unlock()
					fmt.Printf("[📦 Drop] เก็บไอเทมได้: ID %d (จำนวน %d ชิ้น) -> กระเป๋าอัปเดต\n", nameID, amount)
				}
				offset += pLen
				continue
			}
			if pktID == 0x0A18 && offset+14 <= len(data) {
				// ZC_ACCEPT_ENTER3: 14 bytes
				b0 := uint16(data[offset+6])
				b1 := uint16(data[offset+7])
				b2 := uint16(data[offset+8])
				x := int((b0 << 2) | (b1 >> 6))
				y := int(((b1 & 0x3F) << 4) | (b2 >> 4))
				if x > 0 && y > 0 {
					c.CurrentX = x
					c.CurrentY = y
					fmt.Printf("\n[📍 พิกัดเกิด 0x0A18] ยืนอยู่ที่ (%d, %d)\n", c.CurrentX, c.CurrentY)

					// ตอบกลับด้วย MapLoaded (0x007D) และ BlockingPlayCancel (0x0447) หลังได้รับพิกัดยืนยันเข้าแมพแล้ว
					_ = c.sendMapPacket(BuildMapLoaded())
					_ = c.sendMapPacket(BuildBlockingPlayCancel())
				}
				offset += 14
				continue
			}
			if pktID == 0x0091 && offset+22 <= len(data) {
				// map_change (เปลี่ยนแมพผ่านวาร์ป บน Map Server เดียวกัน)
				mapNameRaw := string(bytes.TrimRight(data[offset+2:offset+18], "\x00"))
				newX := int(binary.LittleEndian.Uint16(data[offset+18 : offset+20]))
				newY := int(binary.LittleEndian.Uint16(data[offset+20 : offset+22]))
				if len(mapNameRaw) > 0 && newX < 1024 && newY < 1024 {
					isMapChanged := NormalizeMapName(mapNameRaw) != NormalizeMapName(c.CurrentMap)
					c.CurrentMap = mapNameRaw
					c.CurrentX = newX
					c.CurrentY = newY

					if isMapChanged {
						_ = c.sendMapPacket(BuildMapLoaded())
						_ = c.sendMapPacket(BuildBlockingPlayCancel())
						fmt.Printf("\n[🌀 Warp] ข้ามแมพสำเร็จ! เข้าสู่ %s ที่พิกัด (%d, %d)\n", c.CurrentMap, c.CurrentX, c.CurrentY)
					}
					offset += 22
					continue
				}
			}
			if (pktID == 0x0092 && offset+28 <= len(data)) || (pktID == 0x0AC7 && offset+28 <= len(data)) {
				// map_changed ข้าม Zone Server / Map Port ต่างกัน (ZC_NPCACK_SERVERMOVE / SERVERMOVE2)
				mapNameRaw := string(bytes.TrimRight(data[offset+2:offset+18], "\x00"))
				newX := int(binary.LittleEndian.Uint16(data[offset+18 : offset+20]))
				newY := int(binary.LittleEndian.Uint16(data[offset+20 : offset+22]))
				newPort := int(binary.LittleEndian.Uint16(data[offset+26 : offset+28]))

				var serverAddr string
				pktLen := 28
				if pktID == 0x0AC7 && offset+156 <= len(data) {
					pktLen = 156
					serverAddr = string(bytes.TrimRight(data[offset+28:offset+156], "\x00"))
					if strings.Contains(serverAddr, ":") {
						parts := strings.Split(serverAddr, ":")
						if len(parts) >= 2 {
							var p int
							fmt.Sscanf(parts[1], "%d", &p)
							if p > 0 {
								newPort = p
							}
						}
					}
				}
				offset += pktLen

				fmt.Printf("\n[🌀 Server Move (0x%04X)] เปลี่ยน Map Server ข้ามโซนสู่ %s ที่พิกัด (%d, %d) พอร์ต %d | ServerAddr: %q...\n",
					pktID, mapNameRaw, newX, newY, newPort, serverAddr)

				c.CurrentMap = mapNameRaw
				c.CurrentX = newX
				c.CurrentY = newY
				if newPort > 0 {
					c.MapPort = newPort
				}

				if c.MapWs != nil {
					_ = c.MapWs.Close()
				}
				time.Sleep(200 * time.Millisecond)

				if err := c.stepMap(); err != nil {
					fmt.Printf("[!] สลับ Map Server ไม่สำเร็จ: %v\n", err)
					return
				}
				fmt.Printf("[🌀 Server Move] เชื่อมต่อ Map Server %s (พอร์ต %d) สำเร็จ พร้อมเดินทางต่อ!\n", c.CurrentMap, c.MapPort)
				break
			}
			if pktID == 0x007F && offset+6 <= len(data) {
				// received_sync (6 bytes)
				offset += 6
				continue
			}
			if pktID == 0x0086 && offset+8 <= len(data) {
				// ZC_NOTIFY_PLAYERMOVE (8 bytes)
				offset += 8
				continue
			}
			if pktID == 0x0087 && offset+12 <= len(data) {
				// ZC_NOTIFY_PLAYERMOVE / character_moves (12 bytes: Header 2B + StartTime 4B + PosData 6B)
				pData := data[offset+6 : offset+12]
				toX := int(uint16(pData[2]&0x0F)<<6 | uint16(pData[3]&0xFC)>>2)
				toY := int(uint16(pData[3]&0x03)<<8 | uint16(pData[4]))
				if toX > 0 && toY > 0 && toX < 1024 && toY < 1024 {
					c.CurrentX = toX
					c.CurrentY = toY
					if DebugMovement {
						fmt.Printf("\n[👟 Move 0x0087] เซิร์ฟเวอร์ยืนยันก้าวเดินสู่ (%d, %d)\n", toX, toY)
					}
				}
				offset += 12
				continue
			}
			if pktID == 0x00B6 && offset+6 <= len(data) {
				// ZC_NPC_TALK_CLOSE: 6 bytes (Header 2B + NPC ID 4B)
				offset += 6
				continue
			}
			if pktID == 0x00B5 && offset+6 <= len(data) {
				// ZC_NPC_TALK_NEXT: 6 bytes
				offset += 6
				continue
			}
			if pktID == 0x00B7 && offset+4 <= len(data) {
				// ZC_NPC_TALK_RESPONSES: Header 2B + Len 2B + NPC ID 4B + options
				pktLen := int(binary.LittleEndian.Uint16(data[offset+2 : offset+4]))
				offset += pktLen
				continue
			}
			if pktID == 0x00B4 && offset+4 <= len(data) {
				// ZC_NPC_TALK: variable len
				pktLen := int(binary.LittleEndian.Uint16(data[offset+2 : offset+4]))
				offset += pktLen
				continue
			}

			if pktID == PacketRozRecv {
				if offset+4 > len(data) {
					break
				}
				pktLen := int(binary.LittleEndian.Uint16(data[offset+2 : offset+4]))
				if pktLen < 4 || offset+pktLen > len(data) {
					pktLen = len(data) - offset
				}
				pkt := data[offset : offset+pktLen]
				if len(pkt) >= 13 {
					op := pkt[12]
					payload := string(pkt[13:])

					// Op 10: ข้อมูลตัวละคร เลเวล EXP และพิกัดแมพ
					if op == 10 {
						if mapName := extractKV(payload, "map"); mapName != "" {
							c.CurrentMap = mapName
						}

						if name := extractKV(payload, "name"); name != "" {
							c.CharName = name
						}
						if clsStr := extractKV(payload, "cls"); clsStr != "" {
							var cid int
							fmt.Sscanf(clsStr, "%d", &cid)
							c.JobID = uint16(cid)
							c.JobClass = JobIDToName(c.JobID)
						}

						if bl := extractKV(payload, "bl"); bl != "" {
							fmt.Sscanf(bl, "%d", &c.BaseLevel)
						}
						if jl := extractKV(payload, "jl"); jl != "" {
							fmt.Sscanf(jl, "%d", &c.JobLevel)
						}

						if beStr := extractKV(payload, "be"); beStr != "" {
							var be uint64
							fmt.Sscanf(beStr, "%d", &be)
							if c.PrevBaseExp > 0 && be > c.PrevBaseExp {
								c.BaseGain = be - c.PrevBaseExp
							}
							c.PrevBaseExp = be
							c.BaseExp = be
						}
						if bnStr := extractKV(payload, "bn"); bnStr != "" {
							fmt.Sscanf(bnStr, "%d", &c.BaseExpNext)
						}

						if jeStr := extractKV(payload, "je"); jeStr != "" {
							var je uint64
							fmt.Sscanf(jeStr, "%d", &je)
							if c.PrevJobExp > 0 && je > c.PrevJobExp {
								c.JobGain = je - c.PrevJobExp
							}
							c.PrevJobExp = je
							c.JobExp = je
						}
						if jnStr := extractKV(payload, "jn"); jnStr != "" {
							fmt.Sscanf(jnStr, "%d", &c.JobExpNext)
						}
					}

					// Op 12: ค่าเลือด HP, มานา SP, น้ำหนัก Weight, เงิน Zeny
					if op == 12 {
						if hpStr := extractKV(payload, "hp"); hpStr != "" {
							fmt.Sscanf(hpStr, "%d", &c.HP)
							if c.HP <= 0 && c.MaxHP > 0 && !c.IsDead {
								c.IsDead = true
								fmt.Printf("\n[💀 Dead] ตัวละครตาย (HP: %d/%d)! รอ 3 วินาทีแล้วจะคืนชีพกลับจุดเซฟ (0x00B2)...\n", c.HP, c.MaxHP)
								go func() {
									time.Sleep(3 * time.Second)
									c.sendMapPacket(BuildRestart(0))
									time.Sleep(2 * time.Second)
									c.sendMapPacket([]byte{0x47, 0x04}) // 0x0447 Unblock
									c.IsDead = false
								}()
							}
						}
						if mhpStr := extractKV(payload, "mhp"); mhpStr != "" {
							fmt.Sscanf(mhpStr, "%d", &c.MaxHP)
						}
						if spStr := extractKV(payload, "sp"); spStr != "" {
							fmt.Sscanf(spStr, "%d", &c.SP)
						}
						if mspStr := extractKV(payload, "msp"); mspStr != "" {
							fmt.Sscanf(mspStr, "%d", &c.MaxSP)
						}
						if zStr := extractKV(payload, "z"); zStr != "" {
							fmt.Sscanf(zStr, "%d", &c.Zeny)
						}
						if wStr := extractKV(payload, "w"); wStr != "" {
							fmt.Sscanf(wStr, "%d", &c.Weight)
						}
						if mwStr := extractKV(payload, "mw"); mwStr != "" {
							fmt.Sscanf(mwStr, "%d", &c.MaxWeight)
						}
						c.checkWeightAndSupplies()
					}

					// Op 1, 2, 3, 4: สถานะบอทและการตั้งค่าของเซิร์ฟเวอร์
					if op == 1 || op == 2 || op == 3 || op == 4 {
						pos := extractKV(payload, "pos")
						if pos != "" {
							if DebugMovement {
								fmt.Printf("\n[📡 Server Pos Sync] พิกัดจริงบนเซิร์ฟเวอร์: %s\n", pos)
							}
							parts := strings.Split(pos, ";")
							if len(parts) >= 2 {
								fmt.Sscanf(parts[0], "%d", &c.CurrentX)
								fmt.Sscanf(parts[1], "%d", &c.CurrentY)
							}
							if len(parts) >= 4 {
								var tid uint32
								fmt.Sscanf(parts[3], "%d", &tid)
								c.TargetGID = tid
							}
						}
					}
				}
				offset += pktLen
			} else {
				if c.PacketLengths != nil {
					if pLen, ok := c.PacketLengths[pktID]; ok {
						if pLen == -1 {
							if offset+4 <= len(data) {
								vLen := int(binary.LittleEndian.Uint16(data[offset+2 : offset+4]))
								if vLen >= 4 && offset+vLen <= len(data) {
									offset += vLen
									continue
								}
							}
							break
						} else if pLen > 0 {
							if offset+pLen <= len(data) {
								offset += pLen
								continue
							}
							break
						}
					}
				}
				offset++
			}
		}

		// ข้อมูลสเตตัสทั้งหมดถูกส่งต่อไปแสดงผลแบบเรียลไทม์บน Web Dashboard (http://localhost:8080) เรียบร้อยแล้ว
		// ปิดการพิมพ์ซ้ำทุก 2 วินาทีใน Console เพื่อไม่ให้สแปมหน้าจอค่ะ
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
	case 4002:
		return "High Swordman"
	case 4003:
		return "High Mage"
	case 4004:
		return "High Archer"
	case 4005:
		return "High Acolyte"
	case 4006:
		return "High Merchant"
	case 4007:
		return "High Thief"
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
	case 4017:
		return "Professor"
	case 4018:
		return "Stalker"
	case 4019:
		return "Creator"
	case 4020:
		return "Clown"
	case 4021:
		return "Gypsy"
	case 4046:
		return "Taekwon"
	case 4047:
		return "Star Gladiator"
	case 4049:
		return "Soul Linker"
	case 4218:
		return "Kagerou"
	case 4219:
		return "Oboro"
	case 4220:
		return "Rebellion"
	default:
		if id > 0 {
			return fmt.Sprintf("Class #%d", id)
		}
		return "Novice"
	}
}
