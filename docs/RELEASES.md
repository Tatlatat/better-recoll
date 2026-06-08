# Releases

`better-recoll` bây giờ có workflow đóng gói native theo tag trong [`.github/workflows/release-packages.yml`](../.github/workflows/release-packages.yml).

## Workflow hiện tại

- `preview-*` hoặc `v*` tag: build package zip rồi publish GitHub Release.
- `workflow_dispatch`: build package để kiểm tra trên Actions, chưa tạo release.

## Package được xuất

- `macos-arm64`: `sfs`, `sfs-server`, `sfs-app`, kèm `libs/libonnxruntime.dylib`
- `linux-amd64`: `sfs`, `sfs-server`, kèm `libs/libonnxruntime.so`
- `windows-amd64`: `sfs.exe`, `sfs-server.exe`, kèm `libs/onnxruntime.dll`

Model không được bundle trong zip. Máy mới chạy lần đầu cần:

```bash
./sfs setup --light
./sfs-server
```

Windows:

```powershell
.\sfs.exe setup --light
.\sfs-server.exe
```

## Quy trình phát hành

```bash
git tag preview-2026-06-08
git push origin preview-2026-06-08
```

Sau khi workflow xong, tải zip trong GitHub Release tương ứng rồi giải nén trên máy đích để thử nghiệm.
