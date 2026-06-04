package model

// Helper tối giản đọc file .npy (numpy) float32, 1-D hoặc 2-D, C-order,
// little-endian — đủ để load reference_vectors.npy cho parity test.
// Chỉ dùng trong test (không phải code production).

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var npyShapeRe = regexp.MustCompile(`'shape':\s*\(([^)]*)\)`)
var npyDescrRe = regexp.MustCompile(`'descr':\s*'([^']*)'`)

// loadNpyFloat32 đọc .npy 2-D float32, trả [][]float32 (rows × cols).
func loadNpyFloat32(path string) ([][]float32, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) < 10 || string(raw[0:6]) != "\x93NUMPY" {
		return nil, fmt.Errorf("not a npy file: %s", path)
	}
	// version raw[6],raw[7]; header len uint16 at raw[8:10] (v1.0)
	hlen := int(binary.LittleEndian.Uint16(raw[8:10]))
	headerEnd := 10 + hlen
	header := string(raw[10:headerEnd])

	descr := npyDescrRe.FindStringSubmatch(header)
	if descr == nil || !strings.Contains(descr[1], "f4") {
		return nil, fmt.Errorf("expected float32 (f4) descr, got header: %s", header)
	}
	sm := npyShapeRe.FindStringSubmatch(header)
	if sm == nil {
		return nil, fmt.Errorf("no shape in header: %s", header)
	}
	var dims []int
	for _, p := range strings.Split(sm[1], ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		d, err := strconv.Atoi(p)
		if err != nil {
			return nil, err
		}
		dims = append(dims, d)
	}
	var rows, cols int
	switch len(dims) {
	case 1:
		rows, cols = 1, dims[0]
	case 2:
		rows, cols = dims[0], dims[1]
	default:
		return nil, fmt.Errorf("unsupported ndim %d", len(dims))
	}

	data := raw[headerEnd:]
	need := rows * cols * 4
	if len(data) < need {
		return nil, fmt.Errorf("npy data too short: have %d need %d", len(data), need)
	}
	out := make([][]float32, rows)
	idx := 0
	for r := 0; r < rows; r++ {
		out[r] = make([]float32, cols)
		for c := 0; c < cols; c++ {
			bits := binary.LittleEndian.Uint32(data[idx : idx+4])
			out[r][c] = math.Float32frombits(bits)
			idx += 4
		}
	}
	return out, nil
}
