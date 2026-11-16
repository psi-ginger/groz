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
		Action string `json:"action"`
		X      int    `json:"x"`
		Y      int    `json:"y"`
	} `json:"command"`
}

// StartHUDPusher เริ่ม Goroutine ยิงข้อมูลสถานะบอททั้งหมดขึ้น Cloudflare Unified Web HUD
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

			statuses := mgr.GetAllStatuses()
			if len(statuses) == 0 {
				continue
			}

			bodyBytes, err := json.Marshal(statuses)
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

				mgr.mu.RLock()
				for _, b := range mgr.Bots {
					if b != nil && b.Running {
						_ = b.sendMapPacket(BuildMove(tx, ty))
						break
					}
				}
				mgr.mu.RUnlock()
			}
		}
	}()
}
