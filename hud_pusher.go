package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const (
	DefaultHUDEndpoint = "https://cat-site.xryliko.workers.dev"
	DefaultHUDAPIKey   = "b75ddaba8c6779a50caec46e46ec2c1c"
)

type HUDCommandResponse struct {
	Status  string `json:"status"`
	Command *struct {
		Action       string `json:"action"`
		X            int    `json:"x"`
		Y            int    `json:"y"`
		LockMap      string `json:"lock_map"`
		MerchantName string `json:"merchant_name"`
		MerchantLoc  string `json:"merchant_loc"`
		AutoStat     string `json:"auto_stat"`
		AutoSkill    string `json:"auto_skill"`
	} `json:"command"`
}

type BotStatusJSON struct {
	ProfileID    string  `json:"profile_id"`
	ProfileName  string  `json:"profile_name"`
	CharName     string  `json:"char_name"`
	JobClass     string  `json:"job_class"`
	BaseLevel    int     `json:"base_level"`
	JobLevel     int     `json:"job_level"`
	BaseExpPct   float64 `json:"base_exp_pct"`
	JobExpPct    float64 `json:"job_exp_pct"`
	HP           int     `json:"hp"`
	MaxHP        int     `json:"max_hp"`
	HPPct        float64 `json:"hp_pct"`
	SP           int     `json:"sp"`
	MaxSP        int     `json:"max_sp"`
	SPPct        float64 `json:"sp_pct"`
	CurrentMap   string  `json:"current_map"`
	CurrentX     int     `json:"current_x"`
	CurrentY     int     `json:"current_y"`
	TargetGID    uint32  `json:"target_gid"`
	Zeny         int     `json:"zeny"`
	Weight       int     `json:"weight"`
	MaxWeight    int     `json:"max_weight"`
	IsHunting    bool    `json:"is_hunting"`
	StatusText   string  `json:"status_text"`
	LockMap      string  `json:"lock_map"`
	MerchantName string  `json:"merchant_name"`
	MerchantLoc  string  `json:"merchant_loc"`
	AutoStat     string  `json:"auto_stat"`
	AutoSkill    string  `json:"auto_skill"`
}

func (b *SoloBot) GetStatusJSON() BotStatusJSON {
	cName := b.charName
	if cName == "" {
		cName = b.cfg.Username
	}
	jClass := b.jobClass
	if jClass == "" {
		jClass = "Novice"
	}
	status := "ออนไลน์นิ่งในเกม"
	if b.AutoHuntActive {
		status = "กำลังล่าอัตโนมัติ"
	}
	if b.ReturningToMerchant {
		status = "กำลังกลับไปส่งของแม่ค้า"
	}

	return BotStatusJSON{
		ProfileID:    "solo",
		ProfileName:  "SoloBot",
		CharName:     cName,
		JobClass:     jClass,
		BaseLevel:    b.baseLv,
		JobLevel:     b.jobLv,
		HP:           b.hp,
		MaxHP:        b.maxHp,
		SP:           b.sp,
		MaxSP:        b.maxSp,
		CurrentMap:   b.mapName,
		CurrentX:     b.currentX,
		CurrentY:     b.currentY,
		TargetGID:    b.targetGID,
		Zeny:         b.zeny,
		Weight:       b.Weight,
		MaxWeight:    b.MaxWeight,
		IsHunting:    b.AutoHuntActive,
		StatusText:   status,
		LockMap:      b.cfg.LockMap,
		MerchantName: b.cfg.MerchantName,
		MerchantLoc:  b.cfg.MerchantLoc,
		AutoStat:     b.cfg.AutoStat,
		AutoSkill:    b.cfg.AutoSkill,
	}
}

// StartHUDPusher ยิงข้อมูลสถานะ SoloBot ขึ้น Cloudflare Unified Web HUD
func (b *SoloBot) StartHUDPusher(stopChan chan struct{}) {
	endpoint := DefaultHUDEndpoint
	client := &http.Client{Timeout: 3 * time.Second}

	fmt.Printf("[🌐 Cloud HUD] เชื่อมต่อ Unified Web HUD สำเร็จ -> %s/ro\n", endpoint)
	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-stopChan:
			return
		case <-ticker.C:
			if !b.running {
				continue
			}

			status := b.GetStatusJSON()
			bodyBytes, err := json.Marshal([]BotStatusJSON{status})
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

			if cmdResp.Command != nil {
				switch cmdResp.Command.Action {
				case "start_hunt":
					fmt.Println("[🌐 Cloud HUD] ได้รับคำสั่ง: เริ่มล่าอัตโนมัติ (Start Hunt)")
					_ = b.TriggerAutoHunt()
				case "stop_hunt":
					fmt.Println("[🌐 Cloud HUD] ได้รับคำสั่ง: หยุดล่าอัตโนมัติ (Stop Hunt)")
					_ = b.StopAutoHunt()
				case "move":
					fmt.Printf("[🌐 Cloud HUD] ได้รับคำสั่งเดินไปที่: (%d, %d)\n", cmdResp.Command.X, cmdResp.Command.Y)
					_ = b.sendMapPacket(BuildMove(cmdResp.Command.X, cmdResp.Command.Y))
				case "update_config":
					fmt.Println("[🌐 Cloud HUD] ได้รับคำสั่ง: อัปเดตการตั้งค่าบอท (Update Config)")
					if cmdResp.Command.LockMap != "" {
						b.cfg.LockMap = cmdResp.Command.LockMap
						fmt.Printf("      -> เปลี่ยน Lock Map เป็น: %s\n", b.cfg.LockMap)
					}
					if cmdResp.Command.MerchantName != "" {
						b.cfg.MerchantName = cmdResp.Command.MerchantName
						b.MerchantName = cmdResp.Command.MerchantName
					}
					if cmdResp.Command.MerchantLoc != "" {
						b.cfg.MerchantLoc = cmdResp.Command.MerchantLoc
					}
					if cmdResp.Command.AutoStat != "" {
						b.cfg.AutoStat = cmdResp.Command.AutoStat
					}
					if cmdResp.Command.AutoSkill != "" {
						b.cfg.AutoSkill = cmdResp.Command.AutoSkill
					}
				}
			}
		}
	}
}
