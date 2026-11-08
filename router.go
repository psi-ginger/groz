package main

import (
	"bufio"
	"os"
	"regexp"
	"strconv"
	"strings"
)

type Portal struct {
	FromMap string
	FromX   int
	FromY   int
	ToMap   string
	ToX     int
	ToY     int
}

type PortalGraph struct {
	Portals    map[string][]Portal
	PortalsLOS map[string]map[string]int // Key: "map x y", Val: map["destX destY"] = distance
}

// NormalizeMapName แปลงชื่อแมพช่องหรือชื่อย่อของ Rozclub ให้เป็นชื่อมาตรฐาน RO
// เช่น pay_d00_b.gat -> pay_dun00, pay_d00_b -> pay_dun00, prt_f01_a -> prt_fild01, izlude_b -> izlude
func NormalizeMapName(raw string) string {
	if raw == "" {
		return ""
	}
	name := strings.TrimSuffix(strings.TrimSpace(raw), ".gat")

	// 1. ตัด channel suffix เช่น _a, _b, _c, ..., _z, _x, _s
	reChan := regexp.MustCompile(`_[a-z]$`)
	base := reChan.ReplaceAllString(name, "")

	// 2. แปลงชื่อย่อดันเจี้ยน เช่น _d00 -> _dun00
	reDun := regexp.MustCompile(`_d(\d{2})$`)
	if reDun.MatchString(base) {
		base = reDun.ReplaceAllString(base, "_dun$1")
	}

	// 3. แปลงชื่อย่อฟิลด์ เช่น _f01 -> _fild01
	reFild := regexp.MustCompile(`_f(\d{2})$`)
	if reFild.MatchString(base) {
		base = reFild.ReplaceAllString(base, "_fild$1")
	}

	// 4. แปลง Nightmare / Illusion เช่น _d03_i -> _dun03
	reIll := regexp.MustCompile(`_d(\d{2})_i$`)
	if reIll.MatchString(name) {
		base = reIll.ReplaceAllString(name, "_dun$1")
	}

	return base
}

func LoadPortalGraph(filename string) (*PortalGraph, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	g := &PortalGraph{
		Portals: make(map[string][]Portal),
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		// เฉพาะประตูวาร์ปทางเดินจริง (Physical Portal) จะมีโครงสร้าง 6 ฟิลด์พอดี
		// หากเกิน 6 ฟิลด์จะเป็น NPC คาฟรา/เรือบิน ที่ต้องใช้ระบบคุยไดอะล็อก
		if len(parts) == 6 {
			fx, _ := strconv.Atoi(parts[1])
			fy, _ := strconv.Atoi(parts[2])
			tx, _ := strconv.Atoi(parts[4])
			ty, _ := strconv.Atoi(parts[5])

			p := Portal{
				FromMap: NormalizeMapName(parts[0]),
				FromX:   fx,
				FromY:   fy,
				ToMap:   NormalizeMapName(parts[3]),
				ToX:     tx,
				ToY:     ty,
			}
			g.Portals[p.FromMap] = append(g.Portals[p.FromMap], p)
		}
	}
	_ = g.LoadPortalsLOS("portalsLOS.txt")
	return g, nil
}

func (g *PortalGraph) LoadPortalsLOS(filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	if g.PortalsLOS == nil {
		g.PortalsLOS = make(map[string]map[string]int)
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 7 {
			srcMap := NormalizeMapName(parts[0])
			srcKey := srcMap + " " + parts[1] + " " + parts[2]
			if g.PortalsLOS[srcKey] == nil {
				g.PortalsLOS[srcKey] = make(map[string]int)
			}
			for i := 3; i+3 < len(parts); i += 4 {
				destKey := parts[i+1] + " " + parts[i+2]
				dist, _ := strconv.Atoi(parts[i+3])
				g.PortalsLOS[srcKey][destKey] = dist
			}
		}
	}
	return nil
}

func (g *PortalGraph) IsWalkableBetween(mapName string, fromX, fromY, toX, toY int) bool {
	if g.PortalsLOS == nil {
		return true
	}
	srcKey := NormalizeMapName(mapName) + " " + strconv.Itoa(fromX) + " " + strconv.Itoa(fromY)
	destKey := strconv.Itoa(toX) + " " + strconv.Itoa(toY)
	if dests, ok := g.PortalsLOS[srcKey]; ok {
		if dist, found := dests[destKey]; found {
			return dist > 0
		}
	}
	return true
}

// FindRoute ค้นหาเส้นทางข้ามแมพด้วย BFS (Breadth-First Search) จาก OpenKore portals.txt
func (g *PortalGraph) FindRoute(fromMap, toMap string) []Portal {
	normFrom := NormalizeMapName(fromMap)
	normTo := NormalizeMapName(toMap)

	if normFrom == normTo {
		return nil
	}

	type queueNode struct {
		mapName string
		path    []Portal
	}

	visited := make(map[string]bool)
	queue := []queueNode{{mapName: normFrom, path: nil}}
	visited[normFrom] = true

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		if curr.mapName == normTo {
			return curr.path
		}

		for _, p := range g.Portals[curr.mapName] {
			normNext := NormalizeMapName(p.ToMap)
			if !visited[normNext] {
				visited[normNext] = true
				newPath := make([]Portal, len(curr.path)+1)
				copy(newPath, curr.path)
				newPath[len(curr.path)] = p
				queue = append(queue, queueNode{mapName: normNext, path: newPath})
			}
		}
	}

	return nil
}

// GetPortalsTo ค้นหาประตูวาร์ปทั้งหมดจาก fromMap ที่นำไปสู่ toMap
func (g *PortalGraph) GetPortalsTo(fromMap, toMap string) []Portal {
	normFrom := NormalizeMapName(fromMap)
	normTo := NormalizeMapName(toMap)
	var list []Portal
	for _, p := range g.Portals[normFrom] {
		if NormalizeMapName(p.ToMap) == normTo {
			list = append(list, p)
		}
	}
	return list
}
