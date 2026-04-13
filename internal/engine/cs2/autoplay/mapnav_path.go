package autoplay

import (
	"container/heap"
	"math"
	"sort"
)

func mapNavEdgeWeight(g *mapNavGraph, a, b int) float64 {
	if g == nil || a < 0 || b < 0 || a >= len(g.nodes) || b >= len(g.nodes) {
		return 1
	}
	dx := g.nodes[b].X - g.nodes[a].X
	dz := g.nodes[b].Z - g.nodes[a].Z
	w := math.Hypot(dx, dz)
	if w < 1 {
		return 1
	}
	return w
}

// aStarPath — кратчайший путь по сумме длин рёбер (меньше «ломаной» чем BFS по числу шагов).
func (g *mapNavGraph) aStarPath(from, to int) []int {
	if g == nil || len(g.nodes) == 0 {
		return nil
	}
	if from < 0 || to < 0 || from >= len(g.nodes) || to >= len(g.nodes) {
		return nil
	}
	if from == to {
		return []int{from}
	}
	inf := math.MaxFloat64 / 4
	gScore := make([]float64, len(g.nodes))
	for i := range gScore {
		gScore[i] = inf
	}
	gScore[from] = 0
	came := make([]int, len(g.nodes))
	for i := range came {
		came[i] = -1
	}
	heur := func(i int) float64 {
		dx := g.nodes[i].X - g.nodes[to].X
		dz := g.nodes[i].Z - g.nodes[to].Z
		return math.Hypot(dx, dz)
	}
	pq := &astarHeap{}
	heap.Init(pq)
	heap.Push(pq, &astarNode{idx: from, g: 0, f: heur(from)})
	for pq.Len() > 0 {
		cur := heap.Pop(pq).(*astarNode)
		u := cur.idx
		if cur.g > gScore[u]+1e-9 {
			continue
		}
		if u == to {
			break
		}
		for _, v := range g.nodes[u].edges {
			if v < 0 || v >= len(g.nodes) {
				continue
			}
			w := mapNavEdgeWeight(g, u, v)
			tent := gScore[u] + w
			if tent < gScore[v] {
				came[v] = u
				gScore[v] = tent
				heap.Push(pq, &astarNode{idx: v, g: tent, f: tent + heur(v)})
			}
		}
	}
	if came[to] == -1 {
		return nil
	}
	path := make([]int, 0, 24)
	for v := to; ; v = came[v] {
		path = append(path, v)
		if v == from {
			break
		}
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

type astarNode struct {
	idx int
	g   float64
	f   float64
}

type astarHeap []*astarNode

func (h astarHeap) Len() int           { return len(h) }
func (h astarHeap) Less(i, j int) bool { return h[i].f < h[j].f }
func (h astarHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *astarHeap) Push(x interface{}) {
	*h = append(*h, x.(*astarNode))
}

func (h *astarHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

// kNearestIndices — k узлов с минимальной дистанцией до (px,pz) в XZ.
func (g *mapNavGraph) kNearestIndices(px, pz float64, k int) []int {
	if g == nil || len(g.nodes) == 0 || k <= 0 {
		return nil
	}
	n := len(g.nodes)
	if k >= n {
		out := make([]int, n)
		for i := range out {
			out[i] = i
		}
		return out
	}
	type pair struct {
		i int
		d float64
	}
	arr := make([]pair, n)
	for i := range g.nodes {
		dx := g.nodes[i].X - px
		dz := g.nodes[i].Z - pz
		arr[i] = pair{i, dx*dx + dz*dz}
	}
	sort.Slice(arr, func(i, j int) bool { return arr[i].d < arr[j].d })
	out := make([]int, 0, k)
	for i := 0; i < k; i++ {
		out = append(out, arr[i].i)
	}
	return out
}

// nearestStartForGoal: среди k ближайших по миру узлов выбираем достижимый к goal с минимальной |p−node|^2.
// Так бот не цепляется за узел «за стеной» относительно цели, если рядом есть лучший старт.
func (g *mapNavGraph) nearestStartForGoal(px, pz float64, goal int, k int, maxDist float64) int {
	if g == nil || len(g.nodes) == 0 {
		return 0
	}
	if goal < 0 || goal >= len(g.nodes) {
		return g.nearestIndex(px, pz)
	}
	if k < 4 {
		k = 4
	}
	cands := g.kNearestIndices(px, pz, k)
	maxSq := maxDist * maxDist
	bestI := -1
	bestSq := math.MaxFloat64
	for _, c := range cands {
		dx := g.nodes[c].X - px
		dz := g.nodes[c].Z - pz
		ds := dx*dx + dz*dz
		if ds > maxSq {
			continue
		}
		if g.bfsPath(c, goal) == nil {
			continue
		}
		if ds < bestSq {
			bestSq, bestI = ds, c
		}
	}
	if bestI >= 0 {
		return bestI
	}
	return g.nearestIndex(px, pz)
}

func (g *mapNavGraph) routeArriveRadius() float64 {
	if g == nil || g.arriveR <= 0 {
		return 185
	}
	return g.arriveR
}
