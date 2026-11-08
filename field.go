package main

import (
	"compress/gzip"
	"container/heap"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
)

// MapField เก็บข้อมูล Grid Walkability ของแมพจากไฟล์ .fld2.gz
type MapField struct {
	Name   string
	Width  int
	Height int
	Data   []byte
}

func LoadMapField(mapName string) (*MapField, error) {
	normName := NormalizeMapName(mapName)

	// ค้นหาไฟล์ .fld2.gz ในโฟลเดอร์ท้องถิ่น หรือ fallback ไป openkore
	candidates := []string{
		filepath.Join("fields", normName+".fld2.gz"),
		filepath.Join("..", "openkore", "fields", normName+".fld2.gz"),
		filepath.Join("..", "openkore", "fields", "rRO", normName+".fld2.gz"),
	}

	var foundPath string
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			foundPath = p
			break
		}
	}

	if foundPath == "" {
		return nil, fmt.Errorf("field file not found for %s", normName)
	}

	f, err := os.Open(foundPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	var header [4]byte
	if _, err := gz.Read(header[:]); err != nil {
		return nil, err
	}

	w := int(binary.LittleEndian.Uint16(header[0:2]))
	h := int(binary.LittleEndian.Uint16(header[2:4]))

	data := make([]byte, w*h)
	totalRead := 0
	for totalRead < len(data) {
		n, err := gz.Read(data[totalRead:])
		if n > 0 {
			totalRead += n
		}
		if err != nil {
			break
		}
	}

	return &MapField{
		Name:   normName,
		Width:  w,
		Height: h,
		Data:   data,
	}, nil
}

// IsWalkable ตรวจสอบว่าพิกัด (x, y) เดินได้หรือไม่ (1 = Walkable, 0 = Wall/Water/Obstacle)
func (f *MapField) IsWalkable(x, y int) bool {
	if f == nil || len(f.Data) == 0 {
		return true // Fallback ถ้าไม่มีไฟล์แมพ
	}
	if x < 0 || x >= f.Width || y < 0 || y >= f.Height {
		return false
	}
	// ใน .fld2: bit 0 (ค่า 1) คือ walkable
	return (f.Data[y*f.Width+x] & 1) != 0
}

// FindNearestWalkable หาช่องเดินได้ที่ใกล้พิกัดเป้าหมายที่สุดในรัศมี maxRadius
func (f *MapField) FindNearestWalkable(targetX, targetY, maxRadius int) (int, int) {
	if f.IsWalkable(targetX, targetY) {
		return targetX, targetY
	}
	for r := 1; r <= maxRadius; r++ {
		for dy := -r; dy <= r; dy++ {
			for dx := -r; dx <= r; dx++ {
				if int(math.Abs(float64(dx))) == r || int(math.Abs(float64(dy))) == r {
					nx, ny := targetX+dx, targetY+dy
					if f.IsWalkable(nx, ny) {
						return nx, ny
					}
				}
			}
		}
	}
	return targetX, targetY
}

// HasLOS ตรวจสอบเส้นตรง (Line of Sight) ว่าไม่มีกำแพงขวางระหว่าง (x0, y0) ถึง (x1, y1) ด้วย Bresenham algorithm
func (f *MapField) HasLOS(x0, y0, x1, y1 int) bool {
	dx := int(math.Abs(float64(x1 - x0)))
	dy := -int(math.Abs(float64(y1 - y0)))
	sx, sy := 1, 1
	if x0 >= x1 {
		sx = -1
	}
	if y0 >= y1 {
		sy = -1
	}
	err := dx + dy
	x, y := x0, y0
	for {
		if !f.IsWalkable(x, y) {
			return false
		}
		if x == x1 && y == y1 {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x += sx
		}
		if e2 <= dx {
			err += dx
			y += sy
		}
	}
	return true
}

// A* Node
type aStarNode struct {
	x, y   int
	g, h   int
	f      int
	index  int
	parent *aStarNode
}

type priorityQueue []*aStarNode

func (pq priorityQueue) Len() int           { return len(pq) }
func (pq priorityQueue) Less(i, j int) bool { return pq[i].f < pq[j].f }
func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}
func (pq *priorityQueue) Push(x interface{}) {
	n := len(*pq)
	item := x.(*aStarNode)
	item.index = n
	*pq = append(*pq, item)
}
func (pq *priorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*pq = old[0 : n-1]
	return item
}

// FindPath คำนวณเส้นทาง A* จาก (startX, startY) ไปยัง (goalX, goalY)
func (f *MapField) FindPath(startX, startY, goalX, goalY int, maxSteps int) [][2]int {
	if f == nil || len(f.Data) == 0 {
		return [][2]int{{goalX, goalY}}
	}

	// ถ้าจุดเป้าหมายเดินไม่ได้ ให้หาช่องข้างๆ ที่เดินได้
	gx, gy := f.FindNearestWalkable(goalX, goalY, 5)

	if startX == gx && startY == gy {
		return nil
	}

	openSet := &priorityQueue{}
	heap.Init(openSet)

	startNode := &aStarNode{
		x: startX,
		y: startY,
		g: 0,
		h: (int(math.Abs(float64(gx-startX))) + int(math.Abs(float64(gy-startY)))) * 10,
	}
	startNode.f = startNode.g + startNode.h
	heap.Push(openSet, startNode)

	visited := make(map[int]int) // key: y*width + x, val: gCost
	visited[startY*f.Width+startX] = 0

	dirs := [][3]int{
		{0, 1, 10}, {0, -1, 10}, {1, 0, 10}, {-1, 0, 10},
		{1, 1, 14}, {1, -1, 14}, {-1, 1, 14}, {-1, -1, 14},
	}

	var bestNode *aStarNode = startNode
	iterations := 0
	if maxSteps <= 0 {
		maxSteps = 8000
	}

	for openSet.Len() > 0 && iterations < maxSteps {
		iterations++
		curr := heap.Pop(openSet).(*aStarNode)

		if curr.h < bestNode.h {
			bestNode = curr
		}

		if curr.x == gx && curr.y == gy {
			bestNode = curr
			break
		}

		for _, d := range dirs {
			nx, ny, cost := curr.x+d[0], curr.y+d[1], d[2]
			if !f.IsWalkable(nx, ny) {
				continue
			}

			// ป้องกันเดินทะลุมุมเฉียงถ้ามีกำแพงขนาบสองข้าง
			if d[0] != 0 && d[1] != 0 {
				if !f.IsWalkable(curr.x+d[0], curr.y) || !f.IsWalkable(curr.x, curr.y+d[1]) {
					continue
				}
			}

			newG := curr.g + cost
			key := ny*f.Width + nx

			if oldG, exists := visited[key]; exists && newG >= oldG {
				continue
			}

			visited[key] = newG
			h := (int(math.Abs(float64(gx-nx))) + int(math.Abs(float64(gy-ny)))) * 10
			neighbor := &aStarNode{
				x:      nx,
				y:      ny,
				g:      newG,
				h:      h,
				f:      newG + h,
				parent: curr,
			}
			heap.Push(openSet, neighbor)
		}
	}

	// Reconstruct path
	var path [][2]int
	curr := bestNode
	for curr != nil && (curr.x != startX || curr.y != startY) {
		path = append([][2]int{{curr.x, curr.y}}, path...)
		curr = curr.parent
	}

	return path
}
