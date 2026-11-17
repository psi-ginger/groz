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
	cloudMode := flag.Bool("cloud", false, "Run in Cloud Multi-Account sync mode")
	flag.Parse()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	if *cloudMode {
		mgr := NewMultiBotManager("", "")
		go func() {
			<-sigChan
			mgr.StopAll()
			os.Exit(0)
		}()
		mgr.StartSyncLoop()
		return
	}

	cfg := LoadConfig(*cfgFile)
	bot := NewSoloBot(cfg)

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
