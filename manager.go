package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Profile struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	CharSlot     byte   `json:"char_slot"`
	LockMap      string `json:"lock_map"`
	MerchantName string `json:"merchant_name"`
	MerchantLoc  string `json:"merchant_loc"`
	AutoHunt     bool   `json:"auto_hunt"`
	ReturnWeight int    `json:"return_weight"`
	MinHPPotion  int    `json:"min_hp_potion"`
	AutoStat     string `json:"auto_stat"`
	AutoSkill    string `json:"auto_skill"`
	Enabled      bool   `json:"enabled"`
}

type BotManager struct {
	ProfilesDir string
	BaseProfile Profile
	Bots        map[string]*BotClient
	Profiles    map[string]Profile
	mu          sync.RWMutex
}

func NewBotManager(profilesDir string) *BotManager {
	_ = os.MkdirAll(profilesDir, 0755)
	mgr := &BotManager{
		ProfilesDir: profilesDir,
		Bots:        make(map[string]*BotClient),
		Profiles:    make(map[string]Profile),
	}
	mgr.LoadBase()
	mgr.LoadAllProfiles()
	return mgr
}

func (m *BotManager) LoadBase() {
	basePath := filepath.Join(m.ProfilesDir, "_base.json")
	data, err := os.ReadFile(basePath)
	if err == nil {
		_ = json.Unmarshal(data, &m.BaseProfile)
	} else {
		m.BaseProfile = Profile{
			ID:           "_base",
			Name:         "Base Template",
			LockMap:      "pay_dun00",
			AutoHunt:     true,
			ReturnWeight: 50,
			MinHPPotion:  10,
			Enabled:      true,
		}
		saveData, _ := json.MarshalIndent(m.BaseProfile, "", "  ")
		_ = os.WriteFile(basePath, saveData, 0644)
	}
}

func (m *BotManager) LoadAllProfiles() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entries, err := os.ReadDir(m.ProfilesDir)
	if err != nil {
		return err
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || e.Name() == "_base.json" {
			continue
		}
		path := filepath.Join(m.ProfilesDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var p Profile
		if err := json.Unmarshal(data, &p); err == nil {
			// บังคับให้ Profile ID ยึดตามชื่อไฟล์เสมอ ป้องกันกรณีก๊อปปี้ไฟล์แล้ว ID ชนกันใน Map
			p.ID = strings.TrimSuffix(e.Name(), ".json")
			m.mergeWithBase(&p)
			m.Profiles[p.ID] = p
		}
	}
	return nil
}

func (m *BotManager) mergeWithBase(p *Profile) {
	if p.LockMap == "" {
		p.LockMap = m.BaseProfile.LockMap
	}
	if p.ReturnWeight == 0 {
		p.ReturnWeight = m.BaseProfile.ReturnWeight
	}
	if p.MinHPPotion == 0 {
		p.MinHPPotion = m.BaseProfile.MinHPPotion
	}
}

func (m *BotManager) SaveProfile(p Profile) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if p.ID == "" {
		baseID := strings.ToLower(strings.ReplaceAll(p.Username, " ", "_"))
		if baseID == "" {
			baseID = strings.ToLower(strings.ReplaceAll(p.Name, " ", "_"))
		}
		if p.CharSlot > 0 {
			p.ID = fmt.Sprintf("%s_slot%d", baseID, p.CharSlot)
		} else {
			p.ID = baseID
		}
	}
	m.mergeWithBase(&p)
	m.Profiles[p.ID] = p

	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(m.ProfilesDir, p.ID+".json")
	return os.WriteFile(path, data, 0644)
}

func (m *BotManager) UpdateQuickSettings(id, lockMap, merchantName, merchantLoc, autoStat, autoSkill string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	p, ok := m.Profiles[id]
	if !ok {
		return fmt.Errorf("profile '%s' not found", id)
	}
	p.LockMap = strings.TrimSpace(lockMap)
	p.MerchantName = strings.TrimSpace(merchantName)
	p.MerchantLoc = strings.TrimSpace(merchantLoc)
	p.AutoStat = strings.TrimSpace(autoStat)
	p.AutoSkill = strings.TrimSpace(autoSkill)
	m.Profiles[id] = p

	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(m.ProfilesDir, p.ID+".json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return err
	}

	// ถ้าบอทกำลังรันอยู่ ปรับค่าทันทีในหน่วยความจำ
	if bot, running := m.Bots[id]; running && bot != nil {
		bot.LockMap = p.LockMap
		bot.MerchantName = p.MerchantName
		bot.MerchantLoc = p.MerchantLoc
		bot.AutoStat = p.AutoStat
		bot.AutoSkill = p.AutoSkill
		if bot.Trade != nil {
			bot.Trade.MerchantName = p.MerchantName
		}
	}
	return nil
}

func (m *BotManager) DeleteProfile(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if bot, ok := m.Bots[id]; ok {
		bot.Stop()
		delete(m.Bots, id)
	}
	delete(m.Profiles, id)
	path := filepath.Join(m.ProfilesDir, id+".json")
	return os.Remove(path)
}

func (m *BotManager) StartBot(id string) error {
	m.mu.Lock()
	p, ok := m.Profiles[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("profile '%s' not found", id)
	}

	if oldBot, exists := m.Bots[id]; exists && oldBot.Running {
		m.mu.Unlock()
		return nil // กำลังรันอยู่แล้ว
	}

	bot := NewBotClientFromProfile(p)
	m.Bots[id] = bot
	m.mu.Unlock()

	go func() {
		for {
			m.mu.RLock()
			p, exists := m.Profiles[id]
			bot, hasBot := m.Bots[id]
			m.mu.RUnlock()
			if !exists || !hasBot || !p.Enabled {
				break
			}

			fmt.Printf("\n[🚀 Start Bot] เริ่มต้นบอทโปรไฟล์: %s (%s)\n", p.Name, p.Username)
			if err := bot.StartLoginFlow(); err != nil {
				fmt.Printf("\n[!] บอท %s หลุดการเชื่อมต่อ: %v\n", p.Name, err)
			}
			bot.Stop() // เคลียร์ Goroutines และ Connection ของตัวเก่าให้เกลี้ยง ป้องกันผีหลอกวิ่งซ้อนในแรม

			m.mu.RLock()
			_, stillActive := m.Bots[id]
			m.mu.RUnlock()
			if !stillActive {
				break
			}

			fmt.Printf("\n[🔄 Auto-Reconnect] อีก 5 วินาทีจะเชื่อมต่อใหม่โดยอัตโนมัติ (%s)...\n", p.Name)
			time.Sleep(5 * time.Second)

			m.mu.Lock()
			newBot := NewBotClientFromProfile(p)
			m.Bots[id] = newBot
			bot = newBot
			m.mu.Unlock()
		}
	}()

	return nil
}

func (m *BotManager) StopBot(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if bot, ok := m.Bots[id]; ok {
		bot.Stop()
		delete(m.Bots, id)
		fmt.Printf("\n[⏹️ Stop Bot] สั่งหยุดบอทโปรไฟล์: %s\n", id)
	}
}

func (m *BotManager) StartAll() {
	m.mu.RLock()
	var toStart []string
	for id, p := range m.Profiles {
		if p.Enabled {
			toStart = append(toStart, id)
		}
	}
	m.mu.RUnlock()

	for i, id := range toStart {
		if i > 0 {
			time.Sleep(2500 * time.Millisecond) // หน่วงเวลา 2.5 วินาทีระหว่างแต่ละไอดี
		}
		_ = m.StartBot(id)
	}
}

func (m *BotManager) GetAllProfiles() []Profile {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var list []Profile
	for _, p := range m.Profiles {
		list = append(list, p)
	}
	return list
}

func (m *BotManager) GetAllStatuses() []BotStatusJSON {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var keys []string
	for id := range m.Profiles {
		keys = append(keys, id)
	}
	sort.Strings(keys)

	var list []BotStatusJSON
	for _, id := range keys {
		p := m.Profiles[id]
		bot, isRunning := m.Bots[p.ID]
		if !isRunning || bot == nil {
			list = append(list, BotStatusJSON{
				ProfileID:   p.ID,
				ProfileName: p.Name,
				CurrentMap:  p.LockMap,
				StatusText:  "⏸️ ปิดอยู่",
				IsHunting:   false,
				LockMap:      p.LockMap,
				MerchantName: p.MerchantName,
				MerchantLoc:  p.MerchantLoc,
				AutoStat:     p.AutoStat,
				AutoSkill:    p.AutoSkill,
			})
			continue
		}

		var hpPct, spPct, basePct, jobPct float64
		if bot.MaxHP > 0 {
			hpPct = float64(bot.HP) / float64(bot.MaxHP) * 100
		}
		if bot.MaxSP > 0 {
			spPct = float64(bot.SP) / float64(bot.MaxSP) * 100
		}
		if bot.BaseExpNext > 0 {
			basePct = float64(bot.BaseExp) / float64(bot.BaseExpNext) * 100
		}
		if bot.JobExpNext > 0 {
			jobPct = float64(bot.JobExp) / float64(bot.JobExpNext) * 100
		}

		statusText := "🔍 ลาดตระเวน"
		if bot.ReturningToMerchant {
			statusText = fmt.Sprintf("📦 ไปหาแม่ค้า %s (%s)", p.MerchantName, p.MerchantLoc)
		} else if NormalizeMapName(bot.CurrentMap) != NormalizeMapName(p.LockMap) {
			statusText = fmt.Sprintf("🧭 เดินทางสู่ %s", p.LockMap)
		} else if bot.TargetGID > 0 {
			statusText = fmt.Sprintf("⚔️ ตีมอน #%d", bot.TargetGID)
		} else if bot.AutoHuntActive {
			statusText = "⚔️ กำลังฟาร์ม"
		}

		charName := bot.CharName
		if charName == "" {
			charName = p.Name
		}
		jobClass := bot.JobClass
		if jobClass == "" {
			jobClass = "Novice"
		}

		list = append(list, BotStatusJSON{
			ProfileID:   p.ID,
			ProfileName: p.Name,
			CharName:    charName,
			JobClass:    jobClass,
			BaseLevel:   bot.BaseLevel,
			JobLevel:    bot.JobLevel,
			BaseExpPct:  basePct,
			JobExpPct:   jobPct,
			BaseGain:    bot.BaseGain,
			JobGain:     bot.JobGain,
			HP:          bot.HP,
			MaxHP:       bot.MaxHP,
			HPPct:       hpPct,
			SP:          bot.SP,
			MaxSP:       bot.MaxSP,
			SPPct:       spPct,
			CurrentMap:  strings.TrimSuffix(bot.CurrentMap, ".gat"),
			CurrentX:    bot.CurrentX,
			CurrentY:    bot.CurrentY,
			TargetGID:   bot.TargetGID,
			Zeny:        bot.Zeny,
			Weight:      bot.Weight / 10,
			MaxWeight:   bot.MaxWeight / 10,
			IsHunting:   bot.Running,
			StatusText:   statusText,
			LockMap:      p.LockMap,
			MerchantName: p.MerchantName,
			MerchantLoc:  p.MerchantLoc,
			AutoStat:     p.AutoStat,
			AutoSkill:    p.AutoSkill,
		})
	}
	return list
}
