package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	DefaultHUDEndpoint = "https://cat-site.xryliko.workers.dev"
	DefaultHUDAPIKey   = "b75ddaba8c6779a50caec46e46ec2c1c"
)

type ROPayload struct {
	CharName   string `json:"char_name"`
	JobClass   string `json:"job_class"`
	BaseLevel  int    `json:"base_level"`
	JobLevel   int    `json:"job_level"`
	HP         int    `json:"hp"`
	MaxHP      int    `json:"max_hp"`
	SP         int    `json:"sp"`
	MaxSP      int    `json:"max_sp"`
	CurrentMap string `json:"current_map"`
	CurrentX   int    `json:"current_x"`
	CurrentY   int    `json:"current_y"`
	Zeny       int    `json:"zeny"`
	StatusText string `json:"status_text"`
	MapWidth   int    `json:"map_width"`
	MapHeight  int    `json:"map_height"`
}

type HUDCommandResponse struct {
	Status  string `json:"status"`
	Command *struct {
		Action string `json:"action"`
		X      int    `json:"x"`
		Y      int    `json:"y"`
	} `json:"command"`
}

// StartHUDPusher เริ่ม Goroutine ยิงข้อมูลสถานะขึ้น Cloudflare Unified Web HUD
func StartHUDPusher(mgr *BotManager, endpoint string) {
	if endpoint == "" {
		endpoint = DefaultHUDEndpoint
	}
	client := &http.Client{Timeout: 3 * time.Second}

	go func() {
		fmt.Printf("[🌐 Cloud HUD] เชื่อมต่อ Unified Web HUD สำเร็จ -> %s/ro\n", endpoint)
		ticker := time.NewTicker(1500 * time.Millisecond)
		defer ticker.Stop()

		for range ticker.C {
			if mgr == nil {
				continue
			}

			mgr.mu.RLock()
			var activeBot *BotClient
			var activeProfile Profile
			for id, b := range mgr.Bots {
				if b != nil && b.Running {
					activeBot = b
					activeProfile = mgr.Profiles[id]
					break
				}
			}
			mgr.mu.RUnlock()

			if activeBot == nil {
				continue
			}

			mapW, mapH := 400, 400
			if activeBot.CurrentField != nil {
				if activeBot.CurrentField.Width > 0 {
					mapW = activeBot.CurrentField.Width
				}
				if activeBot.CurrentField.Height > 0 {
					mapH = activeBot.CurrentField.Height
				}
			}

			charName := activeBot.CharName
			if charName == "" {
				charName = activeProfile.Name
			}
			jobClass := activeBot.JobClass
			if jobClass == "" {
				jobClass = "Novice"
			}

			statusText := "🔍 ลาดตระเวน"
			if activeBot.ReturningToMerchant {
				statusText = fmt.Sprintf("📦 ไปหาแม่ค้า %s", activeProfile.MerchantName)
			} else if NormalizeMapName(activeBot.CurrentMap) != NormalizeMapName(activeProfile.LockMap) {
				statusText = fmt.Sprintf("🧭 เดินทางสู่ %s", activeProfile.LockMap)
			} else if activeBot.TargetGID > 0 {
				statusText = fmt.Sprintf("⚔️ ตีมอน #%d", activeBot.TargetGID)
			} else if activeBot.AutoHuntActive {
				statusText = "⚔️ กำลังฟาร์ม"
			}

			payload := ROPayload{
				CharName:   charName,
				JobClass:   jobClass,
				BaseLevel:  activeBot.BaseLevel,
				JobLevel:   activeBot.JobLevel,
				HP:         activeBot.HP,
				MaxHP:      activeBot.MaxHP,
				SP:         activeBot.SP,
				MaxSP:      activeBot.MaxSP,
				CurrentMap: strings.TrimSuffix(NormalizeMapName(activeBot.CurrentMap), ".gat"),
				CurrentX:   activeBot.CurrentX,
				CurrentY:   activeBot.CurrentY,
				Zeny:       activeBot.Zeny,
				StatusText: statusText,
				MapWidth:   mapW,
				MapHeight:  mapH,
			}

			bodyBytes, err := json.Marshal(payload)
			if err != nil {
				continue
			}

			req, err := http.NewRequest(http.MethodPost, endpoint+"/ro/api/update", bytes.NewReader(bodyBytes))
			if err != nil {
				continue
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-API-Key", DefaultHUDAPIKey)
			req.Header.Set("Authorization", "Bearer "+DefaultHUDAPIKey)

			resp, err := client.Do(req)
			if err != nil {
				continue
			}

			var cmdResp HUDCommandResponse
			_ = json.NewDecoder(resp.Body).Decode(&cmdResp)
			resp.Body.Close()

			if cmdResp.Command != nil && cmdResp.Command.Action == "move" {
				tx := cmdResp.Command.X
				ty := cmdResp.Command.Y
				fmt.Printf("\n🕹️ [Remote HUD] ได้รับคำสั่งคลิกเดินจากหน้าเว็บ -> พิกัด (%d, %d)\n", tx, ty)
				_ = activeBot.sendMapPacket(BuildMove(tx, ty))
			}
		}
	}()
}
