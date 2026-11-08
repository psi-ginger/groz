# Rozclub Native Go Multi-Bot

ไคลเอนต์และระบบจัดการบอทอัตโนมัติความเร็วสูงสำหรับ **Rozclub (WebRO)** เขียนด้วยภาษา Go เพียว 100% 
ทำงานแบบ Headless ไม่ต้องเปิดเบราว์เซอร์ กินแรมต่ำเพียง ~60MB ต่อตัว พร้อม Web HUD แดชบอร์ดและเรดาร์แผนที่แบบ Real-time

---

## ✨ ฟีเจอร์เด่น (Key Features)

- **Pure Headless WSS Client:** เชื่อมต่อตรงสู่ Login, Char, และ Map Server ผ่าน WSS โดยไม่ต้องพึ่งพา Chromium หรือ Electron
- **Stream Cipher Handshake:** รองรับการถอดรหัส Xoroshiro128** (`Ec` Cipher) บนแมพฟิลด์ (พอร์ต 5122) แบบอัตโนมัติ
- **A\* Pathfinding & LOS:** ระบบเดินอัจฉริยะ A\* พร้อมการตรวจ Line-of-Sight (Bresenham) ถอยก้าวหลบมุมตึก/กำแพง
- **Dijkstra Multi-Map Router:** คำนวณเส้นทางข้ามแมพหลายทอดจาก Prontera สู่ Payon พร้อมระบบเลือกวาร์ปที่เข้าถึงได้จริง (คัดทิ้งวาร์ปหน้าผาปิด)
- **Delegated AutoHunt:** มอบหมายระบบฟาร์มให้ Auto-Play ของเซิร์ฟเวอร์ (`0x0CF0`) จัดการเอง พฤติกรรมในเกมเนียนตาเหมือนคนเล่นจริง
- **Web HUD & Radar Mini-Map:** ควบคุมบอทผ่านมือถือ/คอมพิวเตอร์ที่ `http://localhost:8080` พร้อม Mini-Map เรดาร์พิกัดสด
- **Self-Contained & Portable:** มีไฟล์แผนที่ (`fields/`) และตารางแพ็กเก็ต (`tables/`) ในตัว สามารถก๊อปปี้ไปรันบน VPS / Linux / Docker ได้ทันที

---

## 🚀 วิธีเริ่มต้นใช้งาน (Quick Start)

### 1. รันบนเครื่อง
```powershell
# รันผ่าน Go โดยตรง
go run .

# หรือรันผ่าน Batch file
.\start-go.bat
```

### 2. เปิดหน้าควบคุม (Web HUD)
- บนคอมพิวเตอร์: `http://localhost:8080`
- บนมือถือ (Wi-Fi เดียวกัน): `http://<IP-เครื่องบอท>:8080`

### 3. เพิ่มโปรไฟล์บอท
- กดปุ่ม **`+ เพิ่มโปรไฟล์`** ที่หน้า Web HUD หรือ
- คัดลอกเทมเพลต `profiles/_base.json` เป็น `profiles/<ชื่อโปรไฟล์>.json` แล้วใส่ `username`, `password`, `lock_map`

---

## 📁 โครงสร้างโปรเจกต์

```text
rozclub-go/
├── client.go         # วงรอบการทำงานของบอท, การเดิน GPS, และ Packet Parser
├── cipher.go         # อัลกอริทึม Stream Cipher (Xoroshiro128** / Ec)
├── field.go          # ตัวอ่านแผนที่ .fld2.gz, ตรวจสอบ Walkability, และอัลกอริทึม A*
├── router.go         # ระบบค้นหาเส้นทางข้ามแมพและคัดเลือกประตูวาร์ป
├── minimap.go        # ตัวสร้างภาพเรดาร์ PNG และจุดวาร์ปสำหรับแดชบอร์ด
├── web.go            # HTTP Server และ REST API สำหรับควบคุมบอท
├── dashboard.html    # หน้าแดชบอร์ด Web HUD พร้อม Canvas Mini-Map
├── manager.go        # ระบบคุมหลายโปรไฟล์พร้อมกัน (Multi-Bot Coordinator)
├── fields/           # ตารางข้อมูลกริดแผนที่ 1,114 แมพ (.fld2.gz)
├── tables/           # ตารางความยาวแพ็กเก็ต (recvpackets)
└── profiles/         # โฟลเดอร์เก็บไฟล์ตั้งค่าตัวละคร (มี .gitignore คุ้มครองรหัสผ่าน)
```
