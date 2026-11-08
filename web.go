package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

//go:embed dashboard.html
var dashboardHTML string

type BotStatusJSON struct {
	ProfileID   string  `json:"profile_id"`
	ProfileName string  `json:"profile_name"`
	CharName    string  `json:"char_name"`
	JobClass    string  `json:"job_class"`
	BaseLevel   int     `json:"base_level"`
	JobLevel    int     `json:"job_level"`
	BaseExpPct  float64 `json:"base_exp_pct"`
	JobExpPct   float64 `json:"job_exp_pct"`
	BaseGain    uint64  `json:"base_gain"`
	JobGain     uint64  `json:"job_gain"`
	HP          int     `json:"hp"`
	MaxHP       int     `json:"max_hp"`
	HPPct       float64 `json:"hp_pct"`
	SP          int     `json:"sp"`
	MaxSP       int     `json:"max_sp"`
	SPPct       float64 `json:"sp_pct"`
	CurrentMap  string  `json:"current_map"`
	CurrentX    int     `json:"current_x"`
	CurrentY    int     `json:"current_y"`
	TargetGID   uint32  `json:"target_gid"`
	Zeny        int     `json:"zeny"`
	Weight      int     `json:"weight"`
	MaxWeight   int     `json:"max_weight"`
	IsHunting   bool    `json:"is_hunting"`
	StatusText  string  `json:"status_text"`
	LockMap     string  `json:"lock_map"`
	NPCSell     string  `json:"npc_sell"`
	NPCStore    string  `json:"npc_store"`
	AutoStat    string  `json:"auto_stat"`
	AutoSkill   string  `json:"auto_skill"`
}

var (
	globalMgr   *BotManager
	botStatusMu sync.RWMutex
)

func StartWebServer(mgr *BotManager, port int) {
	globalMgr = mgr

	http.HandleFunc("/", handleDashboard)
	http.HandleFunc("/api/status", handleAPIStatus)
	http.HandleFunc("/api/profiles", handleAPIProfiles)
	http.HandleFunc("/api/profiles/save", handleAPISaveProfile)
	http.HandleFunc("/api/profiles/quick-update", handleAPIQuickUpdate)
	http.HandleFunc("/api/profiles/delete", handleAPIDeleteProfile)
	http.HandleFunc("/api/bots/start", handleAPIBotStart)
	http.HandleFunc("/api/bots/stop", handleAPIBotStop)
	http.HandleFunc("/api/map/image", handleAPIMapImage)
	http.HandleFunc("/api/map/info", handleAPIMapInfo)

	addr := fmt.Sprintf("0.0.0.0:%d", port)
	go func() {
		if err := http.ListenAndServe(addr, nil); err != nil {
			fmt.Printf("\n[Web HUD] เกิดข้อผิดพลาด: %v\n", err)
		}
	}()
	fmt.Printf("[🌐 Web HUD] แดชบอร์ดพร้อมใช้งานแล้ว:\n")
	fmt.Printf("      - บนคอมพิวเตอร์: http://localhost:%d\n", port)
	fmt.Printf("      - บนมือถือ (Wi-Fi เดียวกัน): http://192.168.0.101:%d\n", port)
}

func handleAPIStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if globalMgr == nil {
		json.NewEncoder(w).Encode([]BotStatusJSON{})
		return
	}
	statuses := globalMgr.GetAllStatuses()
	json.NewEncoder(w).Encode(statuses)
}

func handleAPIProfiles(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if globalMgr == nil {
		json.NewEncoder(w).Encode([]Profile{})
		return
	}
	profiles := globalMgr.GetAllProfiles()
	json.NewEncoder(w).Encode(profiles)
}

func handleAPISaveProfile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var p Profile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if globalMgr != nil {
		_ = globalMgr.SaveProfile(p)
		if p.Enabled {
			_ = globalMgr.StartBot(p.ID)
		}
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleAPIDeleteProfile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	id := r.URL.Query().Get("id")
	if id != "" && globalMgr != nil {
		_ = globalMgr.DeleteProfile(id)
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleAPIBotStart(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	id := r.URL.Query().Get("id")
	if id != "" && globalMgr != nil {
		_ = globalMgr.StartBot(id)
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleAPIBotStop(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	id := r.URL.Query().Get("id")
	if id != "" && globalMgr != nil {
		globalMgr.StopBot(id)
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(dashboardHTML))
}

type QuickUpdateReq struct {
	ID        string `json:"id"`
	LockMap   string `json:"lock_map"`
	NPCSell   string `json:"npc_sell"`
	NPCStore  string `json:"npc_store"`
	AutoStat  string `json:"auto_stat"`
	AutoSkill string `json:"auto_skill"`
}

func handleAPIQuickUpdate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req QuickUpdateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if globalMgr != nil {
		if err := globalMgr.UpdateQuickSettings(req.ID, req.LockMap, req.NPCSell, req.NPCStore, req.AutoStat, req.AutoSkill); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
