package index

import (
	"math"
	"math/rand"
	"sort"
	"sync"
)

const (
	hnswThreshold  = 50000 // Threshold to switch to HNSW
	M              = 16    // Max number of connections per node in layers > 0
	Mmax0          = 32    // Max number of connections per node in layer 0
	efConstruction = 200   // Search depth during graph construction
	// efSearch=300: beam tìm kiếm khi query. Đo được: trên dữ liệu khó (cụm chồng
	// lấn), 100→recall@10 0.863, 300→0.938 (+7.5pp). Tốc độ vẫn thừa: 800µs@200k
	// với ef=100, ef=300 vẫn « 50ms. Đổi chút tốc độ lấy recall cao hơn rõ rệt.
	efSearch = 300 // Search depth during query search

)

var mL = 1.0 / math.Log(float64(M))

// DisableHNSWForTest forces the index to remain in flat-scan mode.
var DisableHNSWForTest bool

// hnswNode represents a node in the HNSW graph.
type hnswNode struct {
	id        int64
	vec       []float32
	level     int
	neighbors [][]int // neighbors[level] = list of neighbor internal indices
}

// searchScratch is reused across queries to avoid allocations.
type searchScratch struct {
	visited    []uint32
	visitedGen uint32
}

var scratchPool = sync.Pool{
	New: func() interface{} {
		return &searchScratch{
			visited: make([]uint32, 0, 1024),
		}
	},
}

// VectorIndex implements a hybrid vector index.
// Below hnswThreshold, it uses a flat exact scan.
// At and above hnswThreshold, it switches to a Hierarchical Navigable Small World (HNSW) graph.
type VectorIndex struct {
	dim        int
	ids        []int64
	vectors    [][]float32
	nodes      []hnswNode
	idToIdx    map[int64]int
	entryPoint int
	maxLevel   int
	rng        *rand.Rand
	rngMu      sync.Mutex
	mu         sync.RWMutex

	// Visited scratch space for single-threaded graphInsert
	visited    []uint32
	visitedGen uint32
}

// NewVectorIndex creates and initializes a new VectorIndex with the specified vector dimension.
func NewVectorIndex(dim int) *VectorIndex {
	return &VectorIndex{
		dim:        dim,
		ids:        make([]int64, 0),
		vectors:    make([][]float32, 0),
		nodes:      nil,
		idToIdx:    make(map[int64]int),
		entryPoint: -1,
		maxLevel:   -1,
		rng:        rand.New(rand.NewSource(42)),
		visited:    make([]uint32, 0),
	}
}

// Add adds a vector to the index.
func (v *VectorIndex) Add(id int64, vec []float32) {
	v.mu.Lock()
	defer v.mu.Unlock()

	// Copy the vector to prevent external modification issues.
	vecCopy := make([]float32, len(vec))
	copy(vecCopy, vec)
	v.ids = append(v.ids, id)
	v.vectors = append(v.vectors, vecCopy)

	if v.nodes != nil && !DisableHNSWForTest {
		v.graphInsert(id, vecCopy)
	} else if len(v.ids) >= hnswThreshold && !DisableHNSWForTest {
		v.buildGraph()
	}
}

// Search searches the vector index for the nearest vectors to the query vector, returning the top-k results.
func (v *VectorIndex) Search(query []float32, k int) []Result {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if len(v.ids) == 0 || k <= 0 {
		return nil
	}

	if v.nodes == nil || DisableHNSWForTest {
		return v.searchFlat(query, k)
	}
	return v.searchHNSW(query, k)
}

// dist calculates 1 - cosine similarity.
// Since vectors are pre-normalized, cosine similarity is dot-product.
func (v *VectorIndex) dist(vec1, vec2 []float32) float32 {
	var dot float32
	limit := len(vec1)
	if len(vec2) < limit {
		limit = len(vec2)
	}
	if limit == 0 {
		return 1.0
	}

	v1 := vec1[:limit]
	v2 := vec2[:limit]

	// Unroll the loop by 8 for performance.
	n := limit
	for n >= 8 {
		dot += v1[0]*v2[0] + v1[1]*v2[1] + v1[2]*v2[2] + v1[3]*v2[3] +
			v1[4]*v2[4] + v1[5]*v2[5] + v1[6]*v2[6] + v1[7]*v2[7]
		v1 = v1[8:]
		v2 = v2[8:]
		n -= 8
	}
	for i := 0; i < n; i++ {
		dot += v1[i] * v2[i]
	}

	d := 1.0 - dot
	if d < 0 {
		return 0
	}
	return d
}

// randomLevel generates a random level for a new node.
func (v *VectorIndex) randomLevel() int {
	v.rngMu.Lock()
	r := v.rng.Float64()
	v.rngMu.Unlock()
	if r == 0 {
		r = 1e-9
	}
	level := int(math.Floor(-math.Log(r) * mL))
	return level
}

// buildGraph constructs the HNSW graph from the current flat store.
func (v *VectorIndex) buildGraph() {
	v.nodes = make([]hnswNode, 0, len(v.ids))
	v.idToIdx = make(map[int64]int, len(v.ids))
	v.entryPoint = -1
	v.maxLevel = -1

	for i, id := range v.ids {
		v.graphInsert(id, v.vectors[i])
	}
}

// greedySearch performs a heap-free greedy search for ef=1 layers (Algorithm 2).
func (v *VectorIndex) greedySearch(q []float32, epIdx int, layer int) int {
	currIdx := epIdx
	currDist := v.dist(q, v.nodes[currIdx].vec)

	for {
		changed := false
		cNode := &v.nodes[currIdx]
		if layer >= len(cNode.neighbors) {
			break
		}
		neighbors := cNode.neighbors[layer]
		for _, eIdx := range neighbors {
			distQE := v.dist(q, v.nodes[eIdx].vec)
			if distQE < currDist {
				currDist = distQE
				currIdx = eIdx
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return currIdx
}

// graphInsert inserts a vector into the HNSW graph (Malkov Algorithm 1).
func (v *VectorIndex) graphInsert(id int64, vec []float32) {
	l := v.randomLevel()
	idx := len(v.nodes)

	newNode := hnswNode{
		id:        id,
		vec:       vec,
		level:     l,
		neighbors: make([][]int, l+1),
	}
	v.nodes = append(v.nodes, newNode)
	v.idToIdx[id] = idx

	// Ensure visited slice is large enough
	numNodes := len(v.nodes)
	if len(v.visited) < numNodes {
		newVisited := make([]uint32, numNodes*2)
		copy(newVisited, v.visited)
		v.visited = newVisited
	}

	if len(v.nodes) == 1 {
		v.entryPoint = idx
		v.maxLevel = l
		return
	}

	currEp := v.entryPoint
	maxL := v.maxLevel

	// 1. Greedy search from maxLevel down to l+1
	for lc := maxL; lc >= l+1; lc-- {
		currEp = v.greedySearch(vec, currEp, lc)
	}

	ep := []int{currEp}

	// 2. Search layer min(maxLevel, l) down to 0
	startL := maxL
	if l < maxL {
		startL = l
	}

	for lc := startL; lc >= 0; lc-- {
		W := v.searchLayer(vec, ep, efConstruction, lc, v.visited, &v.visitedGen)
		neighbors := v.selectNeighborsHeuristic(vec, W.data, M)
		v.nodes[idx].neighbors[lc] = neighbors

		for _, nIdx := range neighbors {
			for len(v.nodes[nIdx].neighbors) <= lc {
				v.nodes[nIdx].neighbors = append(v.nodes[nIdx].neighbors, make([]int, 0))
			}
			v.nodes[nIdx].neighbors[lc] = append(v.nodes[nIdx].neighbors[lc], idx)

			maxM := M
			if lc == 0 {
				maxM = Mmax0
			}
			if len(v.nodes[nIdx].neighbors[lc]) > maxM {
				nNeighbors := make([]distPair, 0, len(v.nodes[nIdx].neighbors[lc]))
				for _, neighborIdx := range v.nodes[nIdx].neighbors[lc] {
					neighborNode := &v.nodes[neighborIdx]
					dist := v.dist(v.nodes[nIdx].vec, neighborNode.vec)
					nNeighbors = append(nNeighbors, distPair{idx: neighborIdx, dist: dist})
				}
				pruned := v.selectNeighborsHeuristic(v.nodes[nIdx].vec, nNeighbors, maxM)
				v.nodes[nIdx].neighbors[lc] = pruned
			}
		}

		ep = make([]int, len(W.data))
		for i, p := range W.data {
			ep[i] = p.idx
		}
	}

	if l > maxL {
		v.entryPoint = idx
		v.maxLevel = l
	}
}

// searchLayer searches a single layer for nearest neighbors (Malkov Algorithm 2).
func (v *VectorIndex) searchLayer(q []float32, entryPoints []int, ef int, layer int, visited []uint32, pVisitedGen *uint32) *maxHeap {
	*pVisitedGen++
	visitedGen := *pVisitedGen
	if visitedGen == 0 {
		for i := range visited {
			visited[i] = 0
		}
		*pVisitedGen = 1
		visitedGen = 1
	}

	candidates := &minHeap{}
	W := &maxHeap{}

	for _, epIdx := range entryPoints {
		visited[epIdx] = visitedGen
		node := &v.nodes[epIdx]
		dist := v.dist(q, node.vec)
		pair := distPair{idx: epIdx, dist: dist}
		candidates.Push(pair)
		W.Push(pair)
	}

	for candidates.Len() > 0 {
		c := candidates.Pop()
		f := W.Peek()
		if c.dist > f.dist {
			break
		}

		cNode := &v.nodes[c.idx]
		if layer >= len(cNode.neighbors) {
			continue
		}
		for _, eIdx := range cNode.neighbors[layer] {
			if visited[eIdx] == visitedGen {
				continue
			}
			visited[eIdx] = visitedGen

			eNode := &v.nodes[eIdx]
			distQE := v.dist(q, eNode.vec)
			f = W.Peek()

			if W.Len() < ef {
				pair := distPair{idx: eIdx, dist: distQE}
				candidates.Push(pair)
				W.Push(pair)
			} else if distQE < f.dist {
				pair := distPair{idx: eIdx, dist: distQE}
				candidates.Push(pair)
				W.data[0] = pair
				W.down(0, len(W.data))
			}
		}
	}

	return W
}

// selectNeighborsHeuristic selects neighbors using the heuristic for diversity (Malkov Algorithm 4).
func (v *VectorIndex) selectNeighborsHeuristic(q []float32, candidates []distPair, m int) []int {
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].dist < candidates[j].dist
	})

	result := make([]int, 0, m)
	for _, e := range candidates {
		if len(result) >= m {
			break
		}
		eNode := &v.nodes[e.idx]
		good := true
		for _, rIdx := range result {
			rNode := &v.nodes[rIdx]
			distER := v.dist(eNode.vec, rNode.vec)
			if distER < e.dist {
				good = false
				break
			}
		}
		if good {
			result = append(result, e.idx)
		}
	}

	// Fallback to fill up to m neighbors if heuristic filtered too many
	if len(result) < m && len(result) < len(candidates) {
		for _, e := range candidates {
			if len(result) >= m {
				break
			}
			exists := false
			for _, rIdx := range result {
				if rIdx == e.idx {
					exists = true
					break
				}
			}
			if !exists {
				result = append(result, e.idx)
			}
		}
	}

	return result
}

// searchFlat performs exact flat scanning.
func (v *VectorIndex) searchFlat(query []float32, k int) []Result {
	results := make([]Result, len(v.ids))
	for i, id := range v.ids {
		storedVec := v.vectors[i]
		var score float32

		limit := len(query)
		if len(storedVec) < limit {
			limit = len(storedVec)
		}
		for j := 0; j < limit; j++ {
			score += query[j] * storedVec[j]
		}

		results[i] = Result{
			ID:    id,
			Score: score,
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].ID < results[j].ID
		}
		return results[i].Score > results[j].Score
	})

	if len(results) > k {
		results = results[:k]
	}
	return results
}

// searchHNSW searches using the HNSW graph.
func (v *VectorIndex) searchHNSW(query []float32, k int) []Result {
	if len(v.nodes) == 0 {
		return nil
	}

	scratch := scratchPool.Get().(*searchScratch)
	defer scratchPool.Put(scratch)

	numNodes := len(v.nodes)
	if len(scratch.visited) < numNodes {
		newVisited := make([]uint32, numNodes*2)
		copy(newVisited, scratch.visited)
		scratch.visited = newVisited
	}

	currEp := v.entryPoint
	maxL := v.maxLevel

	for lc := maxL; lc >= 1; lc-- {
		currEp = v.greedySearch(query, currEp, lc)
	}

	ep := []int{currEp}

	ef := efSearch
	if k > ef {
		ef = k
	}

	W := v.searchLayer(query, ep, ef, 0, scratch.visited, &scratch.visitedGen)

	results := make([]Result, 0, W.Len())
	for W.Len() > 0 {
		pair := W.Pop()
		results = append(results, Result{
			ID:    v.nodes[pair.idx].id,
			Score: 1.0 - pair.dist,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].ID < results[j].ID
		}
		return results[i].Score > results[j].Score
	})

	if len(results) > k {
		results = results[:k]
	}
	return results
}

// distPair connects a node internal index with its distance to query.
type distPair struct {
	idx  int
	dist float32
}

// minHeap is a binary min-heap.
type minHeap struct {
	data []distPair
}

func (h *minHeap) Len() int { return len(h.data) }

func (h *minHeap) Push(x distPair) {
	h.data = append(h.data, x)
	h.up(len(h.data) - 1)
}

func (h *minHeap) Pop() distPair {
	n := len(h.data) - 1
	h.data[0], h.data[n] = h.data[n], h.data[0]
	h.down(0, n)
	x := h.data[n]
	h.data = h.data[:n]
	return x
}

func (h *minHeap) up(i int) {
	for {
		parent := (i - 1) / 2
		if parent == i || h.data[parent].dist <= h.data[i].dist {
			break
		}
		h.data[parent], h.data[i] = h.data[i], h.data[parent]
		i = parent
	}
}

func (h *minHeap) down(i0, n int) {
	i := i0
	for {
		left := 2*i + 1
		if left >= n || left < 0 {
			break
		}
		smallest := left
		if right := left + 1; right < n && h.data[right].dist < h.data[left].dist {
			smallest = right
		}
		if h.data[i].dist <= h.data[smallest].dist {
			break
		}
		h.data[i], h.data[smallest] = h.data[smallest], h.data[i]
		i = smallest
	}
}

// maxHeap is a binary max-heap.
type maxHeap struct {
	data []distPair
}

func (h *maxHeap) Len() int { return len(h.data) }

func (h *maxHeap) Push(x distPair) {
	h.data = append(h.data, x)
	h.up(len(h.data) - 1)
}

func (h *maxHeap) Pop() distPair {
	n := len(h.data) - 1
	h.data[0], h.data[n] = h.data[n], h.data[0]
	h.down(0, n)
	x := h.data[n]
	h.data = h.data[:n]
	return x
}

func (h *maxHeap) Peek() distPair {
	return h.data[0]
}

func (h *maxHeap) up(i int) {
	for {
		parent := (i - 1) / 2
		if parent == i || h.data[parent].dist >= h.data[i].dist {
			break
		}
		h.data[parent], h.data[i] = h.data[i], h.data[parent]
		i = parent
	}
}

func (h *maxHeap) down(i0, n int) {
	i := i0
	for {
		left := 2*i + 1
		if left >= n || left < 0 {
			break
		}
		largest := left
		if right := left + 1; right < n && h.data[right].dist > h.data[left].dist {
			largest = right
		}
		if h.data[i].dist >= h.data[largest].dist {
			break
		}
		h.data[i], h.data[largest] = h.data[largest], h.data[i]
		i = largest
	}
}
