#!/usr/bin/env bash
# ============================================================================
# Giao việc viết HNSW cho agy + GÁC CỔNG tự động (chống drift).
#
# Cơ chế chống drift KHÔNG dựa vào agy "nhớ" — mà dựa vào TEST RECALL khách quan:
# sau mỗi lần agy làm, script tự chạy gate. Nếu recall chưa đạt, nó đưa CHÍNH
# log lỗi quay lại cho agy và bảo sửa tiếp. Lặp tối đa MAX_ROUNDS vòng.
#
# Bạn chỉ cần chạy:   bash docs/agy-tasks/run-hnsw.sh
# Theo dõi:           tmux không cần — script in tiến độ ra màn hình.
# ============================================================================
set -uo pipefail

REPO="/Users/tatlatat/Documents/level 3"
SPEC="$REPO/docs/agy-tasks/HNSW_SPEC.md"
TARGET="$REPO/internal/index/vector.go"
TESTFILE="$REPO/internal/index/hnsw_test.go"
MAX_ROUNDS=8
cd "$REPO" || exit 1

# Prompt khởi tạo: chỉ agy đọc spec và làm. Ngắn gọn, mọi chi tiết nằm trong spec.
INIT_PROMPT=$(cat <<'EOF'
Bạn là người viết code. Đọc TOÀN BỘ file docs/agy-tasks/HNSW_SPEC.md và thực hiện
ĐÚNG theo nó. Nhiệm vụ: viết lại internal/index/vector.go thành HNSW thuần Go đúng
chuẩn Malkov 2016, và viết internal/index/hnsw_test.go với các test recall/tốc độ
trong spec (mục 5). RÀNG BUỘC: không thêm dependency ngoài (thuần Go, không cgo),
không đổi public API của VectorIndex, recall@10 >= 0.95 và recall@1 >= 0.98.
Làm theo quy trình mục 6 (B1..B7). Khi xong, tự chạy lệnh kiểm thử mục 7 và đảm bảo
TẤT CẢ test PASS. Recall là vua: code chạy mà recall thấp = chưa xong.
EOF
)

# Lệnh GATE: build + vet + chạy test HNSW. In PASS/FAIL + log recall.
gate() {
  echo "=================== GATE (Claude gác cổng) ==================="
  echo "[1/3] build..."
  if ! go build ./... 2>&1 | grep -v "duplicate librar" | grep -q . ; then :; fi
  go build ./... 2>/tmp/gate_build.err
  if [ -s /tmp/gate_build.err ] && grep -qiE "error|cannot|undefined" /tmp/gate_build.err; then
    echo "BUILD FAIL:"; grep -v "duplicate librar" /tmp/gate_build.err | head -20
    return 1
  fi
  echo "    build OK"
  echo "[2/3] vet..."
  go vet ./internal/index/ 2>/tmp/gate_vet.err
  if grep -qiE "error|\.go:" /tmp/gate_vet.err; then
    echo "VET FAIL:"; cat /tmp/gate_vet.err | head; return 1
  fi
  echo "    vet OK"
  echo "[3/3] test recall + speed (có thể mất vài phút)..."
  go test ./internal/index/ -run "TestVectorIndexExact|TestHNSW|TestVectorFlatScan|TestVectorScanScaling" \
     -v -timeout 900s > /tmp/gate_test.log 2>&1
  local rc=$?
  grep -v "duplicate librar" /tmp/gate_test.log | grep -E "recall|--- PASS|--- FAIL|^ok|^FAIL|PASS|FAIL" | tail -30
  if [ $rc -ne 0 ] || grep -q "FAIL" /tmp/gate_test.log; then
    echo "GATE FAIL (rc=$rc)"; return 1
  fi
  echo "GATE PASS ✅"
  return 0
}

# Soạn prompt sửa lỗi: đưa CHÍNH log gate cho agy.
fix_prompt() {
  cat <<EOF
Gate (build/vet/test recall) CHƯA đạt. Đây là log lỗi:

----- LOG -----
$(grep -v "duplicate librar" /tmp/gate_test.log 2>/dev/null | tail -40)
$(cat /tmp/gate_build.err 2>/dev/null | head -10)
---------------

Sửa internal/index/vector.go (và hnsw_test.go nếu test sai cách viết, NHƯNG KHÔNG
được hạ ngưỡng recall trong spec để né). Bám docs/agy-tasks/HNSW_SPEC.md — đặc biệt
mục 4.5 (searchLayer: popMax phải bỏ điểm XA NHẤT, điều kiện dừng đúng) và 4.6
(selectNeighborsHeuristic). Mục tiêu vẫn là recall@10>=0.95, recall@1>=0.98. Sửa
xong tự chạy lại lệnh test mục 7.
EOF
}

echo "### VÒNG 1: giao việc gốc cho agy ###"
agy --print --dangerously-skip-permissions --add-dir "$REPO" --print-timeout 30m \
    --prompt "$INIT_PROMPT" 2>&1 | tail -40

for round in $(seq 1 "$MAX_ROUNDS"); do
  echo ""
  echo "########## GÁC CỔNG sau vòng $round ##########"
  if gate; then
    echo ""
    echo "🎉 HOÀN THÀNH: HNSW đạt recall + tốc độ. Kiểm tra lần cuối toàn bộ suite:"
    go test ./internal/... -timeout 900s 2>&1 | grep -v "duplicate librar" | tail -15
    exit 0
  fi
  if [ "$round" -ge "$MAX_ROUNDS" ]; then
    echo "❌ Hết $MAX_ROUNDS vòng mà gate chưa đạt. Dừng để người xem lại."
    echo "   Log cuối: /tmp/gate_test.log"
    exit 1
  fi
  echo "### Gửi log lỗi lại cho agy (vòng sửa $((round+1))) ###"
  agy --print --dangerously-skip-permissions --add-dir "$REPO" --print-timeout 30m \
      --continue --prompt "$(fix_prompt)" 2>&1 | tail -40
done
