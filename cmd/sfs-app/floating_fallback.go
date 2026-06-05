//go:build !darwin

package main

import "unsafe"

func makeFloating(ptr unsafe.Pointer) {}
func makeNormalWindow(ptr unsafe.Pointer) {}
func centerWindow(ptr unsafe.Pointer) {}
func showWindow(ptr unsafe.Pointer) {}
func hideWindow(ptr unsafe.Pointer) {}
func resizeWindow(ptr unsafe.Pointer, width, height int) {}
