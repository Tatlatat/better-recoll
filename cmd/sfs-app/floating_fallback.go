//go:build !darwin

package main

import "unsafe"

func makeFloating(ptr unsafe.Pointer)                    {}
func makeNormalWindow(ptr unsafe.Pointer)                {}
func centerWindow(ptr unsafe.Pointer)                    {}
func showWindow(ptr unsafe.Pointer)                      {}
func hideWindow(ptr unsafe.Pointer)                      {}
func resizeWindow(ptr unsafe.Pointer, width, height int) {}

// windowVisible: trên non-darwin chưa có cách đọc trạng thái cửa sổ native, trả
// false để toggle hotkey luôn đi nhánh "hiện" (an toàn — không bao giờ ẩn nhầm).
func windowVisible(ptr unsafe.Pointer) bool { return false }
