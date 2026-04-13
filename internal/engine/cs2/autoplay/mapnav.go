package autoplay

import (
	"log"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"time"
)

// mapNavNode — ориентир в мировых XZ (как в GSI player.position; Y — высота, не используем).
type mapNavNode struct {
	X, Z   float64
	Label  string
	Zone   string // логическая зона спавна / участка карты
	edges  []int
}

type mapNavGraph struct {
	nodes []mapNavNode
	// Для графов с сотнями/тысячами узлов — uniform grid nearest O(1) в среднем.
	spatialCell float64
	spatialMinX float64
	spatialMinZ float64
	spatial     map[int64][]int
	// Радиус «приехали к узлу» для tickGraphRoamSteer (0 → см. routeArriveRadius default).
	arriveR float64
}

const mapNavSpatialMinNodes = 96

// Уплотнение рёбер: больше узлов вдоль уже заданных проходов (стены/перепады сама схема не знает — нужны якоря в JSON).
const (
	mapNavDensifyMinEdge  = 85.0  // меньше — чаще вставки, плавнее маршрут вдоль коридоров
	mapNavDensifyMaxParts = 22    // длинное ребро режется на больше сегментов
	mapNavDensifyMaxNodes = 3200  // потолок узлов на карту (BFS/spatial остаются в норме)
)

func packSpatialCell(ix, iz int) int64 {
	// Индексы ячеек малы; uint32 даёт устойчивый ключ для map.
	return (int64(uint32(ix)) << 32) | int64(uint32(iz))
}

// buildSpatialIndex вызывается после загрузки JSON; для малых карт не создаётся.
func (g *mapNavGraph) buildSpatialIndex() {
	if g == nil || len(g.nodes) < mapNavSpatialMinNodes {
		g.spatial = nil
		g.spatialCell = 0
		return
	}
	minX, maxX := g.nodes[0].X, g.nodes[0].X
	minZ, maxZ := g.nodes[0].Z, g.nodes[0].Z
	for i := 1; i < len(g.nodes); i++ {
		n := &g.nodes[i]
		if n.X < minX {
			minX = n.X
		}
		if n.X > maxX {
			maxX = n.X
		}
		if n.Z < minZ {
			minZ = n.Z
		}
		if n.Z > maxZ {
			maxZ = n.Z
		}
	}
	w := maxX - minX
	h := maxZ - minZ
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	// Ячейка ~ сторона квадрата, дающего порядка sqrt(N) ячеек по площади.
	g.spatialMinX, g.spatialMinZ = minX, minZ
	targetCells := math.Sqrt(float64(len(g.nodes)))
	if targetCells < 12 {
		targetCells = 12
	}
	g.spatialCell = math.Max(64, math.Max(w, h)/targetCells)
	g.spatial = make(map[int64][]int, int(targetCells*targetCells))
	for i := range g.nodes {
		n := &g.nodes[i]
		ix := int((n.X - minX) / g.spatialCell)
		iz := int((n.Z - minZ) / g.spatialCell)
		k := packSpatialCell(ix, iz)
		g.spatial[k] = append(g.spatial[k], i)
	}
}

func (g *mapNavGraph) nearestIndexBrute(px, pz float64) int {
	best := 0
	bestD := math.MaxFloat64
	for i := range g.nodes {
		dx := g.nodes[i].X - px
		dz := g.nodes[i].Z - pz
		d := dx*dx + dz*dz
		if d < bestD {
			bestD = d
			best = i
		}
	}
	return best
}

func normalizeMapNameForNav(name string) string {
	n := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(name, `\`, `/`)))
	if i := strings.LastIndex(n, "/"); i >= 0 {
		n = n[i+1:]
	}
	return n
}

// densifyMapNavGraph вставляет узлы на длинных рёбрах. Рёбра задают «коридоры»; промежуточные точки лежат на прямой между якорями.
func densifyMapNavGraph(g *mapNavGraph, minLen float64, maxParts int, maxNodes int) {
	if g == nil || len(g.nodes) < 2 || minLen <= 0 || maxParts < 2 {
		return
	}
	old := g.nodes
	oldCount := len(old)
	edgeKey := func(a, b int) uint64 {
		if a > b {
			a, b = b, a
		}
		return uint64(a)<<32 | uint64(b)
	}
	seen := make(map[uint64]struct{}, oldCount*2)
	pairs := make([][2]int, 0, oldCount)
	for i := 0; i < oldCount; i++ {
		for _, j := range old[i].edges {
			if j < 0 || j >= oldCount {
				continue
			}
			a, b := i, j
			if a > b {
				a, b = b, a
			}
			k := edgeKey(a, b)
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			pairs = append(pairs, [2]int{a, b})
		}
	}

	type draft struct {
		x, z        float64
		label, zone string
	}
	drafts := make([]draft, 0, oldCount+len(pairs))
	for i := range old {
		drafts = append(drafts, draft{old[i].X, old[i].Z, old[i].Label, old[i].Zone})
	}
	adj := make([][]int, oldCount)
	addE := func(a, b int) {
		if a == b {
			return
		}
		adj[a] = append(adj[a], b)
		adj[b] = append(adj[b], a)
	}

	nextIdx := oldCount
	for _, p := range pairs {
		u, v := p[0], p[1]
		dx := old[v].X - old[u].X
		dz := old[v].Z - old[u].Z
		dist := math.Hypot(dx, dz)
		if dist < 1 {
			addE(u, v)
			continue
		}
		parts := int(dist / minLen)
		if parts < 1 {
			parts = 1
		}
		if parts > maxParts {
			parts = maxParts
		}
		if parts == 1 {
			addE(u, v)
			continue
		}
		extra := parts - 1
		if maxNodes > 0 && nextIdx+extra > maxNodes {
			addE(u, v)
			continue
		}
		prev := u
		for s := 1; s < parts; s++ {
			t := float64(s) / float64(parts)
			nx := old[u].X + dx*t
			nz := old[u].Z + dz*t
			zn := old[u].Zone
			if old[u].Zone != old[v].Zone {
				zn = "mid"
			}
			this := nextIdx
			nextIdx++
			lbl := "wp" + strconv.Itoa(this)
			drafts = append(drafts, draft{nx, nz, lbl, zn})
			for len(adj) <= this {
				adj = append(adj, nil)
			}
			addE(prev, this)
			prev = this
		}
		addE(prev, v)
	}

	for len(adj) < len(drafts) {
		adj = append(adj, nil)
	}
	out := make([]mapNavNode, len(drafts))
	for i := range drafts {
		out[i] = mapNavNode{
			X: drafts[i].x, Z: drafts[i].z,
			Label: drafts[i].label,
			Zone:  drafts[i].zone,
		}
	}
	for i := range out {
		out[i].edges = dedupeInts(adj[i])
	}
	g.nodes = out
}

func (g *mapNavGraph) nearestIndex(px, pz float64) int {
	if g == nil || len(g.nodes) == 0 {
		return 0
	}
	if g.spatial == nil || g.spatialCell <= 0 {
		return g.nearestIndexBrute(px, pz)
	}
	ix := int((px - g.spatialMinX) / g.spatialCell)
	iz := int((pz - g.spatialMinZ) / g.spatialCell)
	best := -1
	bestD := math.MaxFloat64
	for dx := -3; dx <= 3; dx++ {
		for dz := -3; dz <= 3; dz++ {
			for _, idx := range g.spatial[packSpatialCell(ix+dx, iz+dz)] {
				n := &g.nodes[idx]
				dx2 := n.X - px
				dz2 := n.Z - pz
				d := dx2*dx2 + dz2*dz2
				if d < bestD {
					bestD = d
					best = idx
				}
			}
		}
	}
	if best >= 0 {
		return best
	}
	return g.nearestIndexBrute(px, pz)
}

func (g *mapNavGraph) nodeZoneByPos(px, pz float64) string {
	i := g.nearestIndex(px, pz)
	if i < 0 || i >= len(g.nodes) {
		return ""
	}
	return g.nodes[i].Zone
}

// randomWaypointWalk случайная «прогулка»: несколько рёбер подряд, без немедленного разворота 180° если есть выбор.
func (g *mapNavGraph) randomWaypointWalk(start int, hops int) []int {
	if g == nil || len(g.nodes) == 0 || hops < 1 {
		return nil
	}
	if start < 0 || start >= len(g.nodes) {
		start = 0
	}
	out := make([]int, 0, hops+1)
	out = append(out, start)
	cur := start
	prev := -1
	for h := 0; h < hops; h++ {
		e := g.nodes[cur].edges
		if len(e) == 0 {
			break
		}
		candidates := e
		if prev >= 0 && len(e) > 1 {
			filtered := make([]int, 0, len(e))
			for _, j := range e {
				if j != prev {
					filtered = append(filtered, j)
				}
			}
			if len(filtered) > 0 {
				candidates = filtered
			}
		}
		next := candidates[rand.Intn(len(candidates))]
		out = append(out, next)
		prev, cur = cur, next
	}
	return out
}

// bfsMaxVisited защита от битого графа при десятках тысяч узлов.
const bfsMaxVisitedMult = 4

// bfsPath кратчайший путь по рёбрам (число шагов). Нет пути — nil.
func (g *mapNavGraph) bfsPath(from, to int) []int {
	if g == nil || len(g.nodes) == 0 {
		return nil
	}
	if from < 0 || to < 0 || from >= len(g.nodes) || to >= len(g.nodes) {
		return nil
	}
	if from == to {
		return []int{from}
	}
	prev := make([]int, len(g.nodes))
	for i := range prev {
		prev[i] = -1
	}
	q := []int{from}
	prev[from] = from
	maxPop := len(g.nodes)*bfsMaxVisitedMult + 1024
	if maxPop > 250000 {
		maxPop = 250000
	}
	for qi := 0; qi < len(q); qi++ {
		if qi > maxPop {
			return nil
		}
		cur := q[qi]
		if cur == to {
			break
		}
		for _, nb := range g.nodes[cur].edges {
			if nb < 0 || nb >= len(g.nodes) {
				continue
			}
			if prev[nb] == -1 {
				prev[nb] = cur
				q = append(q, nb)
			}
		}
	}
	if prev[to] == -1 {
		return nil
	}
	path := make([]int, 0, 16)
	for v := to; ; v = prev[v] {
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

// randomGoalIndex случайная достижимая цель; предпочитает другую zone от старта.
func (g *mapNavGraph) randomGoalIndex(start int) int {
	if g == nil || len(g.nodes) < 2 {
		return start
	}
	z0 := g.nodes[start].Zone
	pool := make([]int, 0, len(g.nodes))
	for i := range g.nodes {
		if i == start {
			continue
		}
		if z0 != "" && g.nodes[i].Zone != "" && g.nodes[i].Zone != z0 {
			pool = append(pool, i)
		}
	}
	if len(pool) == 0 {
		for i := range g.nodes {
			if i != start {
				pool = append(pool, i)
			}
		}
	}
	rand.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	for _, cand := range pool {
		if p := g.bfsPath(start, cand); p != nil && len(p) >= 2 {
			return cand
		}
	}
	return start
}

// meanderAlongPath иногда вставляет узел det между a→b только если есть рёбра a–det и det–b (короткий треугольник).
func (g *mapNavGraph) meanderAlongPath(path []int, pExcursion float64) []int {
	if g == nil || len(path) < 2 {
		return path
	}
	hasEdge := func(u, v int) bool {
		for _, nb := range g.nodes[u].edges {
			if nb == v {
				return true
			}
		}
		return false
	}
	out := make([]int, 0, len(path)+8)
	out = append(out, path[0])
	for i := 0; i < len(path)-1; i++ {
		a, b := path[i], path[i+1]
		if rand.Float64() < pExcursion {
			var detours []int
			for _, det := range g.nodes[a].edges {
				if det == b {
					continue
				}
				if hasEdge(det, b) {
					detours = append(detours, det)
				}
			}
			if len(detours) > 0 {
				out = append(out, detours[rand.Intn(len(detours))])
			}
		}
		out = append(out, b)
	}
	return out
}

func angleDiffRad(a, b float64) float64 {
	d := a - b
	for d > math.Pi {
		d -= 2 * math.Pi
	}
	for d < -math.Pi {
		d += 2 * math.Pi
	}
	return d
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// planGraphNavRoute строит случайный маршрут по графу от ближайшего к GSI-точке узла
// (то же, что «где на миникарте», если включён player_position в GSI).
func (b *CS2Bot) planGraphNavRoute() {
	b.mu.Lock()
	mapName := b.sessionMapName
	px, pz, ok := b.gsiMapX, b.gsiMapZ, b.gsiPosOK
	b.mu.Unlock()
	g := NavGraphForMap(mapName)
	if g == nil {
		b.navRoute = nil
		b.navRouteStep = 0
		return
	}
	mapKey := normalizeMapNameForNav(mapName)
	if !ok {
		b.navRoute = nil
		b.navRouteStep = 0
		return
	}
	startGuess := g.nearestIndex(px, pz)
	zone := g.nodeZoneByPos(px, pz)
	goal := g.randomGoalIndex(startGuess)
	kPick := 28
	if len(g.nodes) > 8000 {
		kPick = 36
	}
	maxSnap := 1150.0
	if len(g.nodes) < 120 {
		maxSnap = 850
	}
	start := g.nearestStartForGoal(px, pz, goal, kPick, maxSnap)
	var route []int
	if path := g.aStarPath(start, goal); path != nil && len(path) >= 2 {
		route = g.meanderAlongPath(path, 0.22+rand.Float64()*0.14)
	} else if path := g.bfsPath(startGuess, goal); path != nil && len(path) >= 2 {
		route = g.meanderAlongPath(path, 0.22+rand.Float64()*0.14)
	} else {
		route = g.randomWaypointWalk(start, 4+rand.Intn(5))
	}
	b.navRoute = route
	b.navRouteStep = 0
	if len(b.navRoute) > 0 {
		gl := ""
		if goal >= 0 && goal < len(g.nodes) {
			gl = g.nodes[goal].Label
		}
		log.Printf("[CS2Bot:%d] nav %s: зона≈%s, цель=%s, маршрут %d узлов (старт %s, вес по длине рёбер)",
			b.display, mapKey, zone, gl, len(b.navRoute), g.nodes[start].Label)
	}
}

// tickGraphRoamSteer возвращает true, если направление задаётся графом (тогда меньше случайного yaw в roam).
func (b *CS2Bot) tickGraphRoamSteer(g *mapNavGraph, px, pz, arrive float64, now time.Time) bool {
	for b.navRouteStep < len(b.navRoute) {
		idx := b.navRoute[b.navRouteStep]
		if idx < 0 || idx >= len(g.nodes) {
			b.navRouteStep++
			continue
		}
		node := g.nodes[idx]
		dx := node.X - px
		dz := node.Z - pz
		if dx*dx+dz*dz < arrive*arrive {
			b.navRouteStep++
			continue
		}
		des := math.Atan2(dz, dx)
		b.navDesiredBear = des

		var spd2 float64
		if !b.navSampleAt.IsZero() {
			dt := now.Sub(b.navSampleAt).Seconds()
			if dt > 0.06 {
				sx := (px - b.navPrevSampleX) / dt
				sz := (pz - b.navPrevSampleZ) / dt
				spd2 = sx*sx + sz*sz
				if spd2 > 625 { // ~25 u/s
					b.navMoveBear = math.Atan2(sz, sx)
				}
			}
		}
		b.navPrevSampleX, b.navPrevSampleZ = px, pz
		b.navSampleAt = now

		mb := b.navMoveBear
		if spd2 < 400 {
			mb += angleDiffRad(des, mb) * 0.08
		}
		err := angleDiffRad(des, mb)
		deg := err * (180 / math.Pi)
		b.navYawTarget = clampFloat(deg*0.9, -92, 92)
		return true
	}
	b.navDesiredBear = 0
	b.planGraphNavRoute()
	return false
}

// --- de_dust2: узлы в типичной шкале CS2 (подправьте по логам GSI при необходимости) ---

func dust2NavGraph() *mapNavGraph {
	// Зоны: T_side, mid, CT_side, long, B — для spawn-подсказки (ближайший узел = «где появился»).
	n := []mapNavNode{
		{-1750, 1650, "t_outer", "T_side", nil},
		{-1250, 1450, "t_ramp", "T_side", nil},
		{-750, 1850, "long_entry", "long", nil},
		{150, 2350, "long", "long", nil},
		{650, 1950, "long_pit", "long", nil},
		{350, 1100, "ct_a", "CT_side", nil},
		{-150, 450, "catwalk", "mid", nil},
		{-700, 50, "mid", "mid", nil},
		{-1150, 150, "low_tunnel", "mid", nil},
		{-1650, 950, "upper_tunnel", "B", nil},
		{-1900, 1350, "b_halls", "B", nil},
		{-250, -250, "ct_chin", "CT_side", nil},
		{250, -1100, "ct_spawn", "CT_side", nil},
		{-650, -950, "ct_b", "CT_side", nil},
		{-1100, 700, "short_b", "B", nil},
	}
	e := [][]int{
		{1},
		{0, 2, 14},
		{1, 3},
		{2, 4, 5},
		{3, 5},
		{3, 4, 6},
		{5, 7, 8},
		{6, 8, 11},
		{6, 7, 9},
		{8, 10, 14},
		{9, 11, 14},
		{7, 12, 13},
		{11, 13},
		{12, 11},
		{1, 9, 10},
	}
	for i := range n {
		n[i].edges = e[i]
	}
	g := &mapNavGraph{nodes: n}
	densifyMapNavGraph(g, mapNavDensifyMinEdge, mapNavDensifyMaxParts, mapNavDensifyMaxNodes)
	g.buildSpatialIndex()
	return g
}

func mirageNavGraph() *mapNavGraph {
	n := []mapNavNode{
		{1050, 250, "t_spawn", "T_side", nil},
		{650, -50, "mid_ramp", "mid", nil},
		{0, -280, "window", "mid", nil},
		{-550, 150, "palace", "mid", nil},
		{-950, 450, "a_conn", "A", nil},
		{-1280, 420, "a_site", "A", nil},
		{-1450, -350, "ct_spawn", "CT_side", nil},
		{450, 650, "market", "mid", nil},
		{180, 1150, "b_site", "B", nil},
		{820, 920, "apps", "T_side", nil},
		{-300, 650, "jungle", "mid", nil},
		{-850, -50, "conn", "mid", nil},
	}
	e := [][]int{
		{1, 9},
		{0, 2, 7},
		{1, 3, 11},
		{2, 4, 10},
		{3, 5, 11},
		{4, 6},
		{5, 11},
		{1, 8, 10},
		{7, 9},
		{0, 8},
		{3, 7},
		{2, 6, 4},
	}
	for i := range n {
		n[i].edges = e[i]
	}
	g := &mapNavGraph{nodes: n}
	densifyMapNavGraph(g, mapNavDensifyMinEdge, mapNavDensifyMaxParts, mapNavDensifyMaxNodes)
	g.buildSpatialIndex()
	return g
}
