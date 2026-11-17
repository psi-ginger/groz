# Rozclub-Solo: Master Architecture & Handoff Blueprint

> **Standalone Pure Go Native TCP RO Bot (LunarisRO Packetver 20250716)**  
> **Status:** ✅ Stable Connection & Cipher Handshake Verified | Clean OOP Modular Architecture

---

## 1. Project Directory & Structure

โค้ดทั้งหมดถูกแยกย่อยเป็นโมดูลเชิงวัตถุ (OOP) เพื่อให้อ่านง่าย ขยายต่อสะดวก และไม่มีโค้ดกองสุมใน `main.go`:

| File | Responsibilities |
| :--- | :--- |
| [main.go](file:///C:/Users/CatsHome/Desktop/webro/rozclub-solo/main.go) | Entry point หลัก: อ่าน flag/config, เริ่มต้น instance และรองรับ Graceful Shutdown (SIGINT/SIGTERM) |
| [bot.go](file:///C:/Users/CatsHome/Desktop/webro/rozclub-solo/bot.go) | นิยาม `SoloBot` struct, constructor `NewSoloBot()`, จัดการ packet table/portals, ฟังก์ชันส่งแพ็กเก็ต (`sendMapPacket`, `SendPM`) และ Lifecycle `Run()` |
| [handshake.go](file:///C:/Users/CatsHome/Desktop/webro/rozclub-solo/handshake.go) | จัดการลำดับการยืนยันตัวตน: `stepLogin()` (MasterLogin `0x0064` -> Session IDs) และ `stepChar()` (CharLogin `0x0065` -> CharSelect `0x0066`) |
| [network.go](file:///C:/Users/CatsHome/Desktop/webro/rozclub-solo/network.go) | จัดการ Map Server TCP: `stepMap()`, Stream Cipher handshake (`0x0CF1`), Packet Framing Loop, และ Heartbeat Ticker (`0x0360`) |
| [handler.go](file:///C:/Users/CatsHome/Desktop/webro/rozclub-solo/handler.go) | จัดการประมวลผล Map Packet: `handleMapPacket()`, PingLive (`0x0B19`), พิกัดเกิด (`0x0073`/`0x02EB`), Auto-Discovery แม่ค้า (`0x09FF`/`0x0857`), ดีลแลกเปลี่ยน (`0x00E5`-`0x00F0`), อัปเดตกระเป๋า (`0x0B09`/`0x0B39`), และ Roz Payload (`0x0CF1` op 12) |
| [movement.go](file:///C:/Users/CatsHome/Desktop/webro/rozclub-solo/movement.go) | แยกระบบนำทางและเดิน: `startGPSLoop()` (A* Pathfinding ข้ามแมพผ่าน `PortalGraph`), `checkWeightAndSupplies()` (วาร์ปกลับเมื่อน้ำหนักเกิน), และ `ParseLocation()` |
| [cipher.go](file:///C:/Users/CatsHome/Desktop/webro/rozclub-solo/cipher.go) | WSS/TCP Stream Cipher (`Xoroshiro128**` + FNV-1a) สองทิศทาง Client/Server |
| [trade.go](file:///C:/Users/CatsHome/Desktop/webro/rozclub-solo/trade.go) | State Machine สำหรับ Closed-Loop Multi-Round Trade กับ Merchant Mule |
| [config.go](file:///C:/Users/CatsHome/Desktop/webro/rozclub-solo/config.go) | โครงสร้างคอนฟิกและฟังก์ชันโหลด JSON |
| [field.go](file:///C:/Users/CatsHome/Desktop/webro/rozclub-solo/field.go) | โหลดไฟล์ตารางแผนที่ `.gat` / `.fld` และ A* Pathfinding |
| [router.go](file:///C:/Users/CatsHome/Desktop/webro/rozclub-solo/router.go) | โหลดกราฟประตูวาร์ประหว่างแมพ (`portals.txt` & `portalsLOS.txt`) |
| [packet.go](file:///C:/Users/CatsHome/Desktop/webro/rozclub-solo/packet.go) | ฟังก์ชันแพ็กไบนารีสำหรับ Opcode ต่างๆ |

---

## 2. Invariants & Handshake Sequencing (ห้ามละเมิดเด็ดขาด)

1. **Stream Cipher Handshake Invariant:**
   - ห้ามส่งแพ็กเก็ตที่เป็น Plaintext หรือ Heartbeat (`0x0360`) ไปยัง Map Server ก่อนที่ฝั่งเซิร์ฟเวอร์จะตอบรับ `0x0CF1` (op 40) เด็ดขาด
   - ตัว Ticker ส่ง Sync จะทำงานเฉพาะเมื่อ `b.cipherActive == true` เท่านั้น
2. **MapLoaded Synchronization:**
   - รอรับพิกัดยืนยัน `0x02EB` (ZC_ACCEPT_ENTER2) ก่อนส่ง `0x007D` (MapLoaded) และ `0x0447` (BlockingCancel)
3. **Auto-Discovery Invariant:**
   - เมื่อสแกนพบแม่ค้า Mayu ในระยะสายตา (`0x09FF`/`0x0857`/`0x09DD`/`0x0915`) บอทจะบันทึก `MerchantGID` อัตโนมัติทันที เพื่อใช้ในการเข้าประชิดและเริ่มดีลโดยไม่ต้องพึ่งพา Whisper สุ่ม
