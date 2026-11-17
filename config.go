package main

import (
	"encoding/json"
	"os"
)

type Config struct {
	Username     string `json:"username"`
	Password     string `json:"password"`
	CharSlot     byte   `json:"char_slot"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	LockMap      string `json:"lock_map"`
	MerchantName string `json:"merchant_name"`
	MerchantGID  uint32 `json:"merchant_gid"`
	MerchantLoc  string `json:"merchant_loc"`
	ReturnWeight int    `json:"return_weight"`
	AutoStat     string `json:"auto_stat"`
	AutoSkill    string `json:"auto_skill"`
}

func DefaultConfig() Config {
	return Config{
		Username:     "minicats",
		Password:     "wasd1111",
		CharSlot:     0,
		Host:         "15.235.236.242",
		Port:         6900,
		LockMap:      "prt_fild08",
		MerchantName: "Mayu",
		MerchantGID:  2900981,
		MerchantLoc:  "payon 167 104",
		ReturnWeight: 50,
		AutoStat:     "str 40,agi 80",
		AutoSkill:    "",
	}
}

func LoadConfig(path string) Config {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		// ถ้าไม่มีไฟล์ ให้เขียน default ออกมาให้อัตโนมัติ
		if out, errM := json.MarshalIndent(cfg, "", "  "); errM == nil {
			_ = os.WriteFile(path, out, 0644)
		}
		return cfg
	}
	_ = json.Unmarshal(data, &cfg)
	if cfg.Host == "" {
		cfg.Host = "15.235.236.242"
	}
	if cfg.Port == 0 {
		cfg.Port = 6900
	}
	if cfg.ReturnWeight == 0 {
		cfg.ReturnWeight = 50
	}
	return cfg
}
