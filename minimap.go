package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"sync"
)

var (
	mapImageCache   = make(map[string][]byte)
	mapImageCacheMu sync.RWMutex

	globalRouter   *PortalGraph
	globalRouterMu sync.Once

	// สีธีม Mini-Map (Dark Modern Tactical Style)
	colorWalkable    = color.RGBA{R: 38, G: 52, B: 69, A: 255}  // พื้นเดินได้: เทาเข้มอมฟ้า
	colorObstacle    = color.RGBA{R: 11, G: 15, B: 20, A: 255}  // กำแพง/สิ่งกีดขวาง: ดำสนิทกลืนกับพื้นหลัง
	colorWaterCliff  = color.RGBA{R: 18, G: 26, B: 36, A: 255}  // น้ำ/หน้าผา: น้ำเงินเข้มมาก
)

func getGlobalRouter() *PortalGraph {
	globalRouterMu.Do(func() {
		rg, err := LoadPortalGraph("portals.txt")
		if err == nil {
			globalRouter = rg
		}
	})
	return globalRouter
}

type MapInfoJSON struct {
	Name    string         `json:"name"`
	Width   int            `json:"width"`
	Height  int            `json:"height"`
	Portals []MapPortalJSON `json:"portals"`
}

type MapPortalJSON struct {
	X    int    `json:"x"`
	Y    int    `json:"y"`
	To   string `json:"to"`
}

// GenerateMapPNG แปลง Grid Walkability ของแมพเป็นไฟล์ภาพ PNG
func GenerateMapPNG(mapName string) ([]byte, int, int, error) {
	normName := NormalizeMapName(mapName)

	mapImageCacheMu.RLock()
	cached, exists := mapImageCache[normName]
	mapImageCacheMu.RUnlock()
	if exists {
		return cached, 0, 0, nil
	}

	fld, err := LoadMapField(normName)
	if err != nil {
		return nil, 0, 0, err
	}

	w, h := fld.Width, fld.Height
	img := image.NewRGBA(image.Rect(0, 0, w, h))

	// ใน RO พิกัด (0,0) อยู่มุมซ้ายล่าง ส่วนในภาพพิกัด (0,0) อยู่มุมซ้ายบน
	// ดังนั้น image Y (py) = h - 1 - y
	for y := 0; y < h; y++ {
		py := h - 1 - y
		rowOffset := y * w
		for x := 0; x < w; x++ {
			cell := fld.Data[rowOffset+x]
			if (cell & 1) != 0 {
				img.SetRGBA(x, py, colorWalkable)
			} else {
				img.SetRGBA(x, py, colorObstacle)
			}
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, 0, 0, err
	}

	pngBytes := buf.Bytes()

	mapImageCacheMu.Lock()
	mapImageCache[normName] = pngBytes
	mapImageCacheMu.Unlock()

	return pngBytes, w, h, nil
}

func handleAPIMapImage(w http.ResponseWriter, r *http.Request) {
	mapName := r.URL.Query().Get("map")
	if mapName == "" {
		http.Error(w, "map name required", http.StatusBadRequest)
		return
	}

	data, _, _, err := GenerateMapPNG(mapName)
	if err != nil {
		http.Error(w, fmt.Sprintf("cannot generate minimap: %v", err), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(data)
}

func handleAPIMapInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	mapName := r.URL.Query().Get("map")
	if mapName == "" {
		http.Error(w, "map name required", http.StatusBadRequest)
		return
	}

	normName := NormalizeMapName(mapName)
	fld, err := LoadMapField(normName)
	if err != nil {
		json.NewEncoder(w).Encode(MapInfoJSON{Name: normName})
		return
	}

	info := MapInfoJSON{
		Name:   normName,
		Width:  fld.Width,
		Height: fld.Height,
	}

	if router := getGlobalRouter(); router != nil {
		for _, p := range router.Portals[normName] {
			info.Portals = append(info.Portals, MapPortalJSON{
				X:  p.FromX,
				Y:  p.FromY,
				To: p.ToMap,
			})
		}
	}

	json.NewEncoder(w).Encode(info)
}
