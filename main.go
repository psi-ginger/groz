package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	cfgFile := flag.String("c", "config.json", "Config file path")
	flag.Parse()

	cfg := LoadConfig(*cfgFile)
	bot := NewSoloBot(cfg)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\n\n[!] กำลังปิดบอทอย่างปลอดภัย...")
		bot.running = false
		if bot.mapConn != nil {
			_ = bot.mapConn.Close()
		}
		os.Exit(0)
	}()

	if err := bot.Run(); err != nil {
		fmt.Printf("\n[❌ เกิดข้อผิดพลาด] %v\n", err)
	}
}
