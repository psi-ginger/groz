package main

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

var (
	DebugMovement = false
)

type SoloBot struct {
	cfg        Config
	accountID  uint32
	sessionID1 uint32
	sessionID2 uint32
	sex        byte
	charID     uint32
	charName   string
	jobClass   string
	baseLv     int
	jobLv      int
	hp         int
	maxHp      int
	sp         int
	maxSp      int
	Weight     int
	MaxWeight  int
	zeny       int
	isDead     bool

	mapName      string
	mapPort      int
	currentX     int
	currentY     int
	targetGID    uint32
	mapConn      net.Conn
	clientCipher *Ec
	serverCipher *Ec
	cipherActive bool
	sendMu       sync.Mutex
	running      bool
	packetLens   map[uint16]int

	// ระบบนำทาง & แผนที่
	currentField *MapField
	router       *PortalGraph
	lastGPSMove  time.Time

	// กระเป๋า & ระบบดีลแม่ค้า
	Inventory           *InventoryManager
	trade               *TradeManager
	ReturningToMerchant bool
	MerchantName        string
	MerchantGID         uint32

	// ระบบ Auto Hunt
	AutoHuntActive bool

	// เครื่องย้ายช่อง (Channel Switch NPC)
	ChannelNpcGID   uint32
	ChannelNpcX     int
	ChannelNpcY     int
	ChannelNpcFound bool
	ChannelTalkStep int       // 0: ยังไม่ได้คุย, 1: เริ่มคุย (รอ Next), 2: รอตอบเมนู
	LastChannelTry  time.Time // กันสแปมคุยรัว

	// ระบบฟื้นคืนชีพ (Respawn)
	LastDeathTime time.Time
}

func NewSoloBot(cfg Config) *SoloBot {
	pktLens := LoadPacketLengths("tables/recvpackets.txt", "../openkore/tables/recvpackets-v20250716.txt", "../rozclub-go/tables/recvpackets-v20250716.txt")

	// โหลดตารางวาร์ป
	var router *PortalGraph
	for _, p := range []string{"portals.txt", "../rozclub-go/portals.txt", "../openkore/tables/portals.txt"} {
		g, err := LoadPortalGraph(p)
		if err == nil && g != nil {
			router = g
			break
		}
	}
	if router != nil {
		for _, p := range []string{"portalsLOS.txt", "../rozclub-go/portalsLOS.txt"} {
			if err := router.LoadPortalsLOS(p); err == nil {
				break
			}
		}
	}

	bot := &SoloBot{
		cfg:          cfg,
		running:      true,
		packetLens:   pktLens,
		router:       router,
		Inventory:    NewInventoryManager(),
		MerchantName: cfg.MerchantName,
		MerchantGID:  cfg.MerchantGID,
	}
	bot.trade = NewTradeManager(bot)
	if bot.trade != nil {
		bot.trade.MerchantName = cfg.MerchantName
		bot.trade.MerchantGID = cfg.MerchantGID
	}
	return bot
}

func (b *SoloBot) dumpHex(data []byte) string {
	limit := len(data)
	if limit > 24 {
		limit = 24
	}
	var sb strings.Builder
	for i := 0; i < limit; i++ {
		sb.WriteString(fmt.Sprintf("%02X ", data[i]))
	}
	if len(data) > limit {
		sb.WriteString("...")
	}
	return sb.String()
}

func (b *SoloBot) SendPM(recipient, message string) {
	_ = b.sendMapPacket(BuildWhisper(recipient, message))
}

func (b *SoloBot) sendMapPacket(pkt []byte) error {
	b.sendMu.Lock()
	defer b.sendMu.Unlock()

	if b.mapConn == nil {
		return fmt.Errorf("map connection is nil")
	}

	out := pkt
	if b.cipherActive && b.clientCipher != nil {
		out = make([]byte, len(pkt))
		copy(out, pkt)
		b.clientCipher.Xor(out)
	}

	_, err := b.mapConn.Write(out)
	return err
}

func (b *SoloBot) TriggerAutoHunt() error {
	b.AutoHuntActive = true
	// ส่งคำสั่งเปิดระบบล่าอัตโนมัติ (OpCode 3, "full")
	huntPkt := BuildRozPacket(OpStart, "full")
	if err := b.sendMapPacket(huntPkt); err != nil {
		return err
	}
	fmt.Printf("[⚔️ AutoHunt] สั่งเปิดระบบล่าอัตโนมัติ 0x0CF0 (Full Mode) เรียบร้อย!\n")
	return nil
}

func (b *SoloBot) StopAutoHunt() error {
	b.AutoHuntActive = false
	// ส่งคำสั่งปิดระบบล่าอัตโนมัติ (OpCode 4)
	stopPkt := BuildRozPacket(OpStop, "")
	if err := b.sendMapPacket(stopPkt); err != nil {
		return err
	}
	fmt.Printf("[🛑 AutoHunt] สั่งปิดระบบล่าอัตโนมัติ 0x0CF0 เรียบร้อย!\n")
	return nil
}

func (b *SoloBot) Run() error {
	fmt.Println("==================================================")
	fmt.Println(" 🚀 Rozclub Solo Standalone Bot (Pure Go TCP)")
	fmt.Printf(" Target: %s:%d | User: %s | LockMap: %s\n", b.cfg.Host, b.cfg.Port, b.cfg.Username, b.cfg.LockMap)
	fmt.Printf(" Merchant: %s (GID: %d) at %s\n", b.cfg.MerchantName, b.cfg.MerchantGID, b.cfg.MerchantLoc)
	fmt.Println("==================================================")

	// [1/4] เชื่อมต่อ Login Server
	fmt.Printf("\n[1/4] 🔑 กำลังเชื่อมต่อ Login Server (%s:%d)...\n", b.cfg.Host, b.cfg.Port)
	if err := b.stepLogin(); err != nil {
		return fmt.Errorf("login server error: %w", err)
	}

	// [2/4] เชื่อมต่อ Char Server
	charPort := 6121
	fmt.Printf("\n[2/4] 👤 กำลังเชื่อมต่อ Char Server (%s:%d)...\n", b.cfg.Host, charPort)
	if err := b.stepChar(charPort); err != nil {
		return fmt.Errorf("char server error: %w", err)
	}

	// [3/4] เชื่อมต่อ Map Server
	fmt.Printf("\n[3/4] 🗺️ กำลังเชื่อมต่อ Map Server (%s:%d)...\n", b.cfg.Host, b.mapPort)
	if err := b.stepMap(); err != nil {
		return fmt.Errorf("map server error: %w", err)
	}

	return nil
}
