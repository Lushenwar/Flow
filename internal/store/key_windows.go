//go:build windows

package store

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	crypt32           = windows.NewLazySystemDLL("crypt32.dll")
	procProtectData   = crypt32.NewProc("CryptProtectData")
	procUnprotectData = crypt32.NewProc("CryptUnprotectData")
	kernel32          = windows.NewLazySystemDLL("kernel32.dll")
	procLocalFree     = kernel32.NewProc("LocalFree")
)

// CRYPTPROTECT_LOCAL_MACHINE — machine scope, so the LocalSystem service and an
// interactive admin both unwrap the same key. Per CLAUDE.md this is tamper-evident,
// not tamper-proof: the key sits on the same disk as the data it signs.
const cryptprotectLocalMachine = 0x4

type dataBlob struct {
	cbData uint32
	pbData *byte
}

func blobOf(b []byte) dataBlob {
	if len(b) == 0 {
		return dataBlob{}
	}
	return dataBlob{cbData: uint32(len(b)), pbData: &b[0]}
}

func (b dataBlob) bytes() []byte {
	out := make([]byte, b.cbData)
	copy(out, unsafe.Slice(b.pbData, b.cbData))
	return out
}

func wrapKey(plain []byte) ([]byte, error) {
	in := blobOf(plain)
	var out dataBlob
	r, _, err := procProtectData.Call(
		uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0,
		cryptprotectLocalMachine,
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, fmt.Errorf("CryptProtectData: %w", err)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return out.bytes(), nil
}

func unwrapKey(wrapped []byte) ([]byte, error) {
	in := blobOf(wrapped)
	var out dataBlob
	r, _, err := procUnprotectData.Call(
		uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0,
		cryptprotectLocalMachine,
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, fmt.Errorf("CryptUnprotectData: %w", err)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return out.bytes(), nil
}
