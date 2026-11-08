package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	fmt.Println("==================================================")
	fmt.Println(" 🚀 Rozclub Native Go Multi-Bot Manager")
	fmt.Println("==================================================")

	mgr := NewBotManager("profiles")
	StartWebServer(mgr, 8080)

	// เริ่มรันบอททุกตัวที่ตั้ง enabled: true ใน profiles/
	mgr.StartAll()

	// รอรับสัญญาณปิดโปรแกรมอย่างปลอดภัย
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\n[!] กำลังปิดระบบบอททั้งหมด...")
	for _, bot := range mgr.Bots {
		bot.Stop()
	}
	fmt.Println("บอทปิดการทำงานเรียบร้อยแล้วค่ะ")
}
