package main

import (
	"encoding/json"
	"os"
)

type Config struct {
	Username string `json:"username"`
	Password string `json:"password"`
	CharSlot byte   `json:"char_slot"` // 0 = ตัวแรก
	Host     string `json:"host"`      // 127.0.0.1 (ws-bridge)
	Port     int    `json:"port"`      // 6900
}

func DefaultConfig() Config {
	return Config{
		Username: "",
		Password: "",
		CharSlot: 0,
		Host:     "127.0.0.1",
		Port:     6900,
	}
}

func LoadConfig(filename string) (Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(filename)
	if err != nil {
		return cfg, err
	}
	err = json.Unmarshal(data, &cfg)
	return cfg, err
}

func SaveConfig(filename string, cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, data, 0644)
}
