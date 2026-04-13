package autoplay

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// SFARM_MAPNAV_DIR — абсолютный путь к каталогу JSON (иначе repo/config/mapnav).
//
// Поддерживаются плотные графы (тысячи узлов, как на скриншотах waypoint-редакторов):
// те же nodes + либо edges [[i,j],…], либо компактно edge_pairs [i,j,i,j,…].
// Координаты обычно берут из экспорта навигации карты (CS nav / сторонние тулзы), не вручную.
//
// При ≥96 узлов строится пространственная сетка для быстрого nearest к позиции GSI; BFS ограничен по шагам.

type mapNavFileJSON struct {
	Map       string `json:"map"`
	Aliases   []string `json:"aliases"`
	Nodes     []struct {
		X     float64 `json:"x"`
		Z     float64 `json:"z"`
		Label string  `json:"label"`
		Zone  string  `json:"zone"`
	} `json:"nodes"`
	Edges     [][]int `json:"edges"`
	EdgePairs []int   `json:"edge_pairs"`
	// Уплотнение рёбер после загрузки (коридоры = только заданные связи; «матрица» карты = плотные точки вдоль проходов).
	DensifyMinEdge  float64 `json:"densify_min_edge"`
	DensifyMaxParts int     `json:"densify_max_parts"`
	DensifyMaxNodes int     `json:"densify_max_nodes"`
	ArriveRadius    float64 `json:"arrive_radius"`
}

func mapNavConfigDir() string {
	if d := strings.TrimSpace(os.Getenv("SFARM_MAPNAV_DIR")); d != "" {
		return d
	}
	exe, err := os.Executable()
	if err != nil {
		exe = "."
	}
	root := FindRepoRoot(filepath.Dir(exe))
	if root == "" {
		if wd, werr := os.Getwd(); werr == nil {
			root = FindRepoRoot(wd)
		}
	}
	if root == "" {
		return filepath.Join("config", "mapnav")
	}
	return filepath.Join(root, "config", "mapnav")
}

var (
	mapNavRegistry   map[string]*mapNavGraph
	mapNavRegistryMu sync.RWMutex
	mapNavLoaded     bool
)

func loadMapNavGraphFromFile(path string) (*mapNavGraph, *mapNavFileJSON, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var raw mapNavFileJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, err
	}
	if len(raw.Nodes) == 0 {
		return nil, nil, os.ErrInvalid
	}
	nodes := make([]mapNavNode, len(raw.Nodes))
	for i := range raw.Nodes {
		nodes[i] = mapNavNode{
			X:     raw.Nodes[i].X,
			Z:     raw.Nodes[i].Z,
			Label: raw.Nodes[i].Label,
			Zone:  raw.Nodes[i].Zone,
		}
	}
	adj := make([][]int, len(nodes))
	addEdge := func(a, b int) {
		if a < 0 || b < 0 || a >= len(nodes) || b >= len(nodes) || a == b {
			return
		}
		adj[a] = append(adj[a], b)
		adj[b] = append(adj[b], a)
	}
	if len(raw.EdgePairs) > 0 {
		if len(raw.EdgePairs)%2 != 0 {
			return nil, nil, os.ErrInvalid
		}
		for i := 0; i < len(raw.EdgePairs); i += 2 {
			addEdge(raw.EdgePairs[i], raw.EdgePairs[i+1])
		}
	} else {
		for _, e := range raw.Edges {
			if len(e) != 2 {
				continue
			}
			addEdge(e[0], e[1])
		}
	}
	for i := range nodes {
		nodes[i].edges = dedupeInts(adj[i])
	}
	g := &mapNavGraph{nodes: nodes}
	minE, maxP, maxN := mapNavDensifyMinEdge, mapNavDensifyMaxParts, mapNavDensifyMaxNodes
	if raw.DensifyMinEdge > 5 {
		minE = raw.DensifyMinEdge
	}
	if raw.DensifyMaxParts >= 2 {
		maxP = raw.DensifyMaxParts
	}
	if raw.DensifyMaxNodes >= 32 {
		maxN = raw.DensifyMaxNodes
	}
	densifyMapNavGraph(g, minE, maxP, maxN)
	if raw.ArriveRadius > 20 {
		g.arriveR = raw.ArriveRadius
	}
	g.buildSpatialIndex()
	return g, &raw, nil
}

func dedupeInts(s []int) []int {
	if len(s) <= 1 {
		return s
	}
	seen := make(map[int]struct{}, len(s))
	out := make([]int, 0, len(s))
	for _, v := range s {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func ensureMapNavRegistryLoaded() {
	mapNavRegistryMu.Lock()
	defer mapNavRegistryMu.Unlock()
	if mapNavLoaded {
		return
	}
	mapNavLoaded = true
	out := make(map[string]*mapNavGraph)
	dir := mapNavConfigDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("[mapnav] config dir %s: %v (встроенные графы для отдельных карт)", dir, err)
	} else {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
				continue
			}
			p := filepath.Join(dir, e.Name())
			g, meta, err := loadMapNavGraphFromFile(p)
			if err != nil {
				log.Printf("[mapnav] %s: %v", p, err)
				continue
			}
			keys := []string{meta.Map}
			keys = append(keys, meta.Aliases...)
			keys = append(keys, strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))
			for _, k := range keys {
				kn := normalizeMapNameForNav(k)
				if kn == "" {
					continue
				}
				out[kn] = g
			}
			log.Printf("[mapnav] loaded %s (%d nodes)", p, len(g.nodes))
		}
	}
	// Встроенный запас, только если для карты нет JSON.
	if out[normalizeMapNameForNav("de_dust2")] == nil {
		g := dust2NavGraph()
		out["de_dust2"], out["dust2"] = g, g
	}
	if out[normalizeMapNameForNav("de_mirage")] == nil {
		g := mirageNavGraph()
		out["de_mirage"], out["mirage"] = g, g
	}
	mapNavRegistry = out
}

// NavGraphForMap: сначала JSON из config/mapnav, иначе встроенный граф (dust2/mirage).
func NavGraphForMap(mapName string) *mapNavGraph {
	ensureMapNavRegistryLoaded()
	n := normalizeMapNameForNav(mapName)
	mapNavRegistryMu.RLock()
	defer mapNavRegistryMu.RUnlock()
	if g := mapNavRegistry[n]; g != nil {
		return g
	}
	return nil
}
