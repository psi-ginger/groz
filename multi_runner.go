package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type RemoteProfile struct {
	Name     string `json:"name"`
	Username string `json:"username"`
	Password string `json:"password"`
	CharSlot byte   `json:"char_slot"`
	LockMap  string `json:"lock_map"`
	Enabled  bool   `json:"enabled"`
}

type MultiBotManager struct {
	endpoint string
	apiKey   string
	bots     map[string]*SoloBot
	mu       sync.Mutex
	stopChan chan struct{}
}

func NewMultiBotManager(endpoint, apiKey string) *MultiBotManager {
	if endpoint == "" {
		endpoint = DefaultHUDEndpoint
	}
	if apiKey == "" {
		apiKey = DefaultHUDAPIKey
	}
	return &MultiBotManager{
		endpoint: endpoint,
		apiKey:   apiKey,
		bots:     make(map[string]*SoloBot),
		stopChan: make(chan struct{}),
	}
}

func (m *MultiBotManager) FetchProfiles() ([]RemoteProfile, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodGet, m.endpoint+"/ro/api/profiles?_t="+fmt.Sprintf("%d", time.Now().UnixMilli()), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", m.apiKey)
	req.Header.Set("Authorization", "Bearer "+m.apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var profiles []RemoteProfile
	if err := json.NewDecoder(resp.Body).Decode(&profiles); err != nil {
		return nil, err
	}
	return profiles, nil
}

func (m *MultiBotManager) StartSyncLoop() {
	fmt.Printf("\n[🌐 Multi-Cloud Sync] เริ่มระบบตรวจสอบโปรไฟล์อัตโนมัติจาก Dashboard: %s\n", m.endpoint)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopChan:
			m.StopAll()
			return
		case <-ticker.C:
			profiles, err := m.FetchProfiles()
			if err != nil {
				continue
			}

			m.mu.Lock()
			activeProfiles := make(map[string]bool)

			for _, p := range profiles {
				if !p.Enabled || p.Username == "" {
					continue
				}
				id := p.Name
				if id == "" {
					id = p.Username
				}
				activeProfiles[id] = true

				if _, exists := m.bots[id]; !exists {
					fmt.Printf("\n[✨ New Profile Detected] พบโปรไฟล์ใหม่จาก Dashboard: '%s' (User: %s) -> กำลังเริ่มบอท...\n", id, p.Username)
					cfg := DefaultConfig()
					cfg.Username = p.Username
					cfg.Password = p.Password
					cfg.CharSlot = p.CharSlot
					if p.LockMap != "" {
						cfg.LockMap = p.LockMap
					}

					bot := NewSoloBot(cfg)
					m.bots[id] = bot

					go func(botID string, b *SoloBot) {
						if err := b.Run(); err != nil {
							fmt.Printf("\n[⚠️ Bot Exited] บอท '%s' หยุดทำงาน: %v\n", botID, err)
						}
						m.mu.Lock()
						delete(m.bots, botID)
						m.mu.Unlock()
					}(id, bot)
				}
			}

			// ปิดบอทที่ถูกลบออกจาก Dashboard
			for botID, bot := range m.bots {
				if !activeProfiles[botID] {
					fmt.Printf("\n[🗑️ Profile Removed] โปรไฟล์ '%s' ถูกลบจาก Dashboard -> กำลังหยุดบอท...\n", botID)
					bot.running = false
					if bot.mapConn != nil {
						_ = bot.mapConn.Close()
					}
					delete(m.bots, botID)
				}
			}
			m.mu.Unlock()
		}
	}
}

func (m *MultiBotManager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	fmt.Println("\n[!] กำลังสั่งหยุดบอททุกตัวในระบบ Multi-Cloud...")
	for id, bot := range m.bots {
		bot.running = false
		if bot.mapConn != nil {
			_ = bot.mapConn.Close()
		}
		delete(m.bots, id)
	}
}
