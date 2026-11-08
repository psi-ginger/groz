package main

import (
	"fmt"
	"math"
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
	wl.KeepIDs[1750] = true // Arrow (ลูกธนูธรรมดา)
	wl.KeepIDs[1751] = true // Silver Arrow
	wl.KeepIDs[1752] = true // Fire Arrow
	wl.KeepIDs[1753] = true // Iron Arrow
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

// เมื่อแม่ค้ารับดีล (0x00E7 ZC_REQ_EXCHANGE / deal_begin)
func (tm *TradeManager) HandleDealBegin() {
	tm.mu.Lock()
	if tm.State != TradeDealInitiating {
		tm.mu.Unlock()
		return
	}
	tm.DealActive = true
	tm.State = TradeDealWaitingHandshake
	tm.mu.Unlock()

	fmt.Println("[🤝 Trade] แม่ค้าตอบรับดีลแล้ว! Pause 3 วินาทีตามสเต็ป 2 เพื่อให้หน้าต่างโหลดพร้อม...")

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

	// Step 3: ถ้าต้องการยา/ลูกธนู ให้ PM สั่งแม่ค้า
	if tm.RestockRequired && tm.MerchantName != "" {
		fmt.Printf("[💬 PM Restock] กระซิบหาแม่ค้า '%s': restock\n", tm.MerchantName)
		tm.Client.SendPM(tm.MerchantName, "restock")
		time.Sleep(1 * time.Second) // รอให้แม่ค้าย้ายของใส่หน้าต่างดีล
	}

	// Step 4: โอนขยะไม่เกิน 10 ชนิด
	tm.State = TradeAddingItems
	batchSize := 10
	if len(tm.PendingTradeList) < batchSize {
		batchSize = len(tm.PendingTradeList)
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

	if !tm.DealActive || tm.State != TradeWaitingLock {
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

	if !tm.DealActive && tm.State != TradeConfirming {
		return
	}

	tm.DealActive = false
	tm.State = TradeRoundComplete
	fmt.Println("[🎉 Trade Complete] การแลกเปลี่ยนรอบนี้สำเร็จสมบูรณ์!")

	// เช็คว่ายังมีขยะเหลือในคิวอีกหรือไม่?
	remaining := len(tm.PendingTradeList)
	if remaining > 0 {
		fmt.Printf("[🔄 Trade Loop] ยังมีขยะเหลืออีก %d ชนิด! รอ 2 วินาทีแล้วเริ่มดีลรอบถัดไป...\n", remaining)
		go func() {
			time.Sleep(2 * time.Second)
			tm.StartDealSession(tm.MerchantGID)
		}()
	} else {
		tm.State = TradeIdle
		fmt.Println("[✨ Town Cycle Done] โอนขยะให้แม่ค้าหมดเกลี้ยงแล้ว! ตัวเบาหวิว พร้อมเดินกลับ LockMap ฟาร์มต่อ!")
	}
}

// เมื่อดีลถูกยกเลิก (0x00EE)
func (tm *TradeManager) HandleDealCancel() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if !tm.DealActive && tm.State == TradeIdle {
		return
	}

	tm.DealActive = false
	tm.State = TradeIdle
	fmt.Println("[⚠️ Trade Cancelled] หน้าต่างดีลถูกยกเลิก")
}
