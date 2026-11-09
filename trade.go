package main

import (
	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"
)

// Item ข้อมูลไอเทมแต่ละชิ้นในกระเป๋า
type Item struct {
	Index      uint16 // ตำแหน่ง index ในกระเป๋า (ใช้สำหรับคำสั่งดีล / ใช้งาน)
	NameID     uint16 // รหัสไอเทม (Item ID เช่น 501 = Red Potion, 1750 = Arrow)
	Amount     int    // จำนวน
	Type       byte   // ประเภท (0 = ใช้หมด, 2 = ขยะ/etc, 3 = อุปกรณ์, 4 = อาวุธ, 5 = การ์ด)
	Equipped   uint16 // สวมใส่อยู่หรือไม่ (>0 คือสวมใส่อยู่)
	Identified bool   // ส่องแว่นขยายแล้วหรือไม่
}

// ItemWhitelist ตารางไอเทมคุ้มครอง (ห้ามโยนให้แม่ค้า)
type ItemWhitelist struct {
	mu      sync.RWMutex
	KeepIDs map[uint16]bool
}

func NewDefaultWhitelist() *ItemWhitelist {
	wl := &ItemWhitelist{
		KeepIDs: make(map[uint16]bool),
	}
	// ไอเทมจำเป็นพื้นฐาน
	wl.KeepIDs[501] = true  // Red Potion
	wl.KeepIDs[502] = true  // Orange Potion
	wl.KeepIDs[503] = true  // Yellow Potion
	wl.KeepIDs[504] = true  // White Potion
	wl.KeepIDs[505] = true  // Blue Potion
	wl.KeepIDs[506] = true  // Green Potion
	wl.KeepIDs[601] = true  // Fly Wing
	wl.KeepIDs[602] = true  // Butterfly Wing
	wl.KeepIDs[611] = true  // Magnifier (แว่นขยาย)
	wl.KeepIDs[645] = true  // Concentration Potion
	wl.KeepIDs[656] = true  // Awakening Potion
	wl.KeepIDs[657] = true  // Berserk Potion
	wl.KeepIDs[1750] = true // Arrow (ลูกธนูธรรมดา)
	wl.KeepIDs[1751] = true // Silver Arrow
	wl.KeepIDs[1752] = true // Fire Arrow
	wl.KeepIDs[1753] = true // Iron Arrow
	wl.KeepIDs[1754] = true // Wind Arrow
	wl.KeepIDs[1755] = true // Stone Arrow
	wl.KeepIDs[1756] = true // Crystal Arrow
	wl.KeepIDs[7033] = true // Novice Potion
	return wl
}

func (w *ItemWhitelist) IsProtected(item *Item) bool {
	if item == nil {
		return true
	}
	// ถ้าสวมใส่อยู่ ห้ามเทรดเด็ดขาด
	if item.Equipped > 0 {
		return true
	}
	// อุปกรณ์สวมใส่ (Type 3), อาวุธ (Type 4), การ์ด (Type 5) ห้ามเทรดทิ้ง
	if item.Type == 3 || item.Type == 4 || item.Type == 5 {
		return true
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.KeepIDs[item.NameID]
}

func (w *ItemWhitelist) AddProtectedID(nameID uint16) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.KeepIDs[nameID] = true
}

// InventoryManager ดูแลกระเป๋าและสิ่งของในตัวบอท
type InventoryManager struct {
	mu    sync.RWMutex
	Items map[uint16]*Item // Key: Index
}

func NewInventoryManager() *InventoryManager {
	return &InventoryManager{
		Items: make(map[uint16]*Item),
	}
}

func (inv *InventoryManager) SetItem(item *Item) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	inv.Items[item.Index] = item
}

func (inv *InventoryManager) RemoveItem(index uint16, amount int) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	if it, exists := inv.Items[index]; exists {
		it.Amount -= amount
		if it.Amount <= 0 {
			delete(inv.Items, index)
		}
	}
}

func (inv *InventoryManager) Clear() {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	inv.Items = make(map[uint16]*Item)
}

func (inv *InventoryManager) CountItem(nameID uint16) int {
	inv.mu.RLock()
	defer inv.mu.RUnlock()
	total := 0
	for _, it := range inv.Items {
		if it.NameID == nameID {
			total += it.Amount
		}
	}
	return total
}

func (inv *InventoryManager) FindItemIndex(nameID uint16) (uint16, bool) {
	inv.mu.RLock()
	defer inv.mu.RUnlock()
	for idx, it := range inv.Items {
		if it.NameID == nameID && it.Amount > 0 {
			return idx, true
		}
	}
	return 0, false
}

// GetItemsToTrade คืนรายการไอเทมขยะที่ต้องเทรดให้แม่ค้า (ไม่รวมของใน Whitelist และของสวมใส่)
func (inv *InventoryManager) GetItemsToTrade(wl *ItemWhitelist) []*Item {
	inv.mu.RLock()
	defer inv.mu.RUnlock()

	var list []*Item
	for _, it := range inv.Items {
		if !wl.IsProtected(it) && it.Amount > 0 {
			list = append(list, it)
		}
	}
	return list
}

// TradeState สถานะใน State Machine ของการเทรด
type TradeState int

const (
	TradeIdle TradeState = iota
	TradeReturningToTown
	TradeApproachingMerchant
	TradeDealInitiating
	TradeDealWaitingHandshake
	TradeAddingItems
	TradeWaitingLock
	TradeConfirming
	TradeRoundComplete
)

// TradeManager คลาสควบคุมการเดินทางกลับเมืองและดีลกับแม่ค้าตาม Step 1-5
type TradeManager struct {
	mu        sync.RWMutex
	Client    *BotClient
	Whitelist *ItemWhitelist

	MerchantName  string
	MerchantGID   uint32
	MerchantMap   string
	MerchantX     int
	MerchantY     int

	State            TradeState
	PendingTradeList []*Item
	CurrentBatch     []*Item
	OtherLocked      bool
	DealActive       bool

	// ค่าควบคุม Restock
	MinHPPotions    int    // เช่น ขั้นต่ำ 20 ขวด
	MinArrows       int    // เช่น ขั้นต่ำ 500 ดอก
	RestockRequired bool
}

func NewTradeManager(client *BotClient) *TradeManager {
	return &TradeManager{
		Client:       client,
		Whitelist:    NewDefaultWhitelist(),
		State:        TradeIdle,
		MinHPPotions: 20,
		MinArrows:    500,
	}
}

// Step 1: ตรวจสอบน้ำหนัก และคำนวณจำนวนรอบดีล (รอบละไม่เกิน 10 ชนิด)
func (tm *TradeManager) CheckWeightAndReturn() bool {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	maxW := tm.Client.MaxWeight
	if maxW <= 0 {
		maxW = 2000
	}
	weightPct := (tm.Client.Weight * 100) / maxW

	// ถ้าน้ำหนักเกิน 50% หรือยาหมด
	hpPotions := tm.Client.Inventory.CountItem(501) // Red Potion
	needRestock := hpPotions < tm.MinHPPotions

	if weightPct < 50 && !needRestock {
		return false
	}

	tm.RestockRequired = needRestock
	tm.State = TradeReturningToTown
	fmt.Printf("[📦 Town Cycle] น้ำหนัก %d%% (ยาเหลือ %d) -> เริ่มกระบวนการกลับเมืองไปหาแม่ค้า\n", weightPct, hpPotions)

	// ใช้ Butterfly Wing (Item ID: 602)
	bwingIdx, hasBwing := tm.Client.Inventory.FindItemIndex(602)
	if hasBwing {
		fmt.Println("[📦 Town Cycle] กดใช้ Butterfly Wing วาร์ปกลับจุดเซฟ...")
		tm.Client.sendMapPacket(BuildUseItem(bwingIdx))
	} else {
		fmt.Println("[📦 Town Cycle] ไม่มี Butterfly Wing! ใช้ Router เดินย้อนวาร์ปกลับเมือง...")
	}
	return true
}

// Step 2: ดีลแม่ค้า & หน่วงเวลา 3 วินาที
func (tm *TradeManager) StartDealSession(merchantGID uint32) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.MerchantGID = merchantGID
	tm.OtherLocked = false
	tm.DealActive = false
	tm.State = TradeDealInitiating

	// ดึงรายการขยะที่จะเทรด
	tm.PendingTradeList = tm.Client.Inventory.GetItemsToTrade(tm.Whitelist)
	totalItems := len(tm.PendingTradeList)
	totalRounds := int(math.Ceil(float64(totalItems) / 10.0))

	fmt.Printf("[🤝 Trade] ยื่นดีลแม่ค้า (GID: %d) | ขยะทั้งหมด %d ชนิด (ต้องดีล %d รอบ)\n",
		merchantGID, totalItems, totalRounds)

	// ส่งคำขอดีล (0x00E4)
	tm.Client.sendMapPacket(BuildDealInitiate(merchantGID))
}

func (tm *TradeManager) IsActive() bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.DealActive || tm.State != TradeIdle
}

// เมื่อหน้าต่างดีลเปิดเรียบร้อย (0x00E7 ZC_ACK_EXCHANGE / deal_begin)
func (tm *TradeManager) HandleDealBegin() {
	tm.mu.Lock()
	tm.DealActive = true
	tm.OtherLocked = false
	tm.State = TradeDealWaitingHandshake
	tm.mu.Unlock()

	fmt.Println("[🤝 Trade] หน้าต่างดีลเปิดเรียบร้อยแล้ว! Pause 3 วินาทีเพื่อให้ฝั่งแม่ค้าพร้อม...")

	go func() {
		time.Sleep(3 * time.Second)
		tm.ExecuteTradeStep3And4()
	}()
}

// Step 3 & 4: เช็ค Restock (ส่ง PM) + โอนขยะขึ้นหน้าต่างดีลไม่เกิน 10 ชนิด
func (tm *TradeManager) ExecuteTradeStep3And4() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if !tm.DealActive {
		return
	}

	merchant := tm.MerchantName
	if merchant == "" && tm.Client != nil {
		merchant = tm.Client.MerchantName
	}

	// Step 3: ถ้าต้องการยา/ลูกธนู ให้ PM สั่งแม่ค้า
	if tm.RestockRequired && merchant != "" {
		fmt.Printf("[💬 PM Restock] กระซิบหาแม่ค้า '%s': restock\n", merchant)
		tm.Client.SendPM(merchant, "restock")
		time.Sleep(1 * time.Second) // รอให้แม่ค้าย้ายของใส่หน้าต่างดีล
	}

	// ถ้า PendingTradeList ยังว่างอยู่ ให้ดึงไอเทมขยะที่เทรดได้ทั้งหมดจากกระเป๋า
	if len(tm.PendingTradeList) == 0 {
		tm.PendingTradeList = tm.Client.Inventory.GetItemsToTrade(tm.Whitelist)
	}

	// Step 4: โอนขยะไม่เกิน 10 ชนิด
	tm.State = TradeAddingItems
	batchSize := 10
	if len(tm.PendingTradeList) < batchSize {
		batchSize = len(tm.PendingTradeList)
	}

	if batchSize == 0 {
		fmt.Println("[ℹ️ Trade] ไม่มีไอเทมขยะที่ต้องส่งมอบในรอบนี้ กด Lock ดีลทันที")
		tm.State = TradeWaitingLock
		tm.Client.sendMapPacket(BuildDealFinalize())
		return
	}

	tm.CurrentBatch = tm.PendingTradeList[:batchSize]
	tm.PendingTradeList = tm.PendingTradeList[batchSize:]

	fmt.Printf("[📤 Trade Step 4] กำลังโอนไอเทมขึ้นหน้าต่างดีล %d ชนิด...\n", len(tm.CurrentBatch))
	for _, it := range tm.CurrentBatch {
		tm.Client.sendMapPacket(BuildDealAddItem(it.Index, uint32(it.Amount)))
		time.Sleep(150 * time.Millisecond) // หน่วงเล็กน้อยเพื่อความเสถียร
	}

	// Step 5: กด Lock (0x00EB)
	fmt.Println("[🔒 Trade Step 5] โอนครบแล้ว -> กด Lock ดีล (0x00EB)...")
	tm.State = TradeWaitingLock
	tm.Client.sendMapPacket(BuildDealFinalize())
}

// เมื่อฝั่งตรงข้ามกด Lock (0x00EC ZC_CONCLUDE_EXCHANGE)
func (tm *TradeManager) HandleDealOtherLocked() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if !tm.DealActive {
		return
	}

	tm.OtherLocked = true
	fmt.Println("[🔒 Trade] แม่ค้ากด Lock ดีลเรียบร้อยแล้ว!")

	// หากเราก็กด Lock แล้ว ให้กดยืนยันจบเทรด (0x00EF)
	if tm.State == TradeWaitingLock {
		tm.State = TradeConfirming
		fmt.Println("[✅ Trade Step 5] ยืนยันการแลกเปลี่ยนรอบสุดท้าย (0x00EF)...")
		tm.Client.sendMapPacket(BuildDealTrade())
	}
}

// เมื่อเทรดสำเร็จสมบูรณ์ (0x00F0 ZC_EXEC_EXCHANGE)
func (tm *TradeManager) HandleDealComplete() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.DealActive = false
	tm.State = TradeRoundComplete
	fmt.Println("[🎉 Trade Complete] การแลกเปลี่ยนรอบนี้สำเร็จสมบูรณ์!")

	// ตัดไอเทมที่ส่งมอบสำเร็จออกจาก Inventory ในตัวบอท
	for _, it := range tm.CurrentBatch {
		tm.Client.Inventory.RemoveItem(it.Index, it.Amount)
	}
	tm.CurrentBatch = nil

	merchant := tm.MerchantName
	if merchant == "" && tm.Client != nil {
		merchant = tm.Client.MerchantName
	}

	// ตรวจสอบไอเทมขยะที่ยังหลงเหลืออยู่ในกระเป๋า
	remaining := tm.Client.Inventory.GetItemsToTrade(tm.Whitelist)
	if len(remaining) > 0 {
		tm.PendingTradeList = remaining
		tm.State = TradeDealInitiating
		rnd := rand.Intn(9000) + 1000
		msg := fmt.Sprintf("deal %d", rnd)
		fmt.Printf("[🔄 Trade Loop] ยังมีขยะเหลืออีก %d ชนิด! ส่ง PM '%s' หา '%s' รอ 2 วินาทีเริ่มรอบถัดไป...\n", len(remaining), msg, merchant)
		go func(mName, pmMsg string) {
			time.Sleep(2 * time.Second)
			tm.Client.SendPM(mName, pmMsg)
		}(merchant, msg)
	} else {
		// เคลียร์หมดเกลี้ยงแล้ว!
		tm.PendingTradeList = nil
		tm.State = TradeIdle
		if tm.Client != nil {
			tm.Client.ReturningToMerchant = false
		}
		rnd := rand.Intn(9000) + 1000
		msg := fmt.Sprintf("done %d", rnd)
		fmt.Printf("[✨ Town Cycle Done] ส่ง PM '%s' หาแม่ค้า '%s' เรียบร้อย! ตัวเบาหวิว พร้อมเดินกลับ LockMap ฟาร์มต่อ!\n", msg, merchant)
		if merchant != "" {
			tm.Client.SendPM(merchant, msg)
		}
	}
}

// เมื่อดีลถูกยกเลิก (0x00EE)
func (tm *TradeManager) HandleDealCancel() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.DealActive = false
	tm.OtherLocked = false
	tm.State = TradeIdle
	tm.CurrentBatch = nil
	fmt.Println("[⚠️ Trade Cancelled] หน้าต่างดีลถูกยกเลิก พร้อมเริ่มใหม่เมื่อพร้อม")
}
