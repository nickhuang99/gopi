// memfdprobe: does a PATH-based GGUF loader (llama.cpp/whisper.cpp family)
// accept an anonymous memfd via /proc/self/fd/N?
//
// This is the escape hatch for the FunASR path: whisper has
// whisper_init_from_buffer (see the PoC), but llama.cpp's model loader takes a
// file path. If it mmaps /proc/self/fd/N happily, an embedded blob can stay
// off disk entirely.
//
// Strictly zero-dep: the memfd syscall is raw Go assembly-free — we use
// syscall.MemfdCreate? (not in the stdlib) so we implement it via SYS_MEMFD_CREATE.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

const (
	sysMemfdCreate = 319 // x86_64
	mfdCloexec     = 0x0001
	chunkBytes     = 1 << 20 // 1 MiB copy chunks (Rule 1: named)
)

func memfdCreate(name string) (int, error) {
	p, err := syscall.BytePtrFromString(name)
	if err != nil {
		return -1, err
	}
	r, _, errno := syscall.Syscall(sysMemfdCreate, uintptr(unsafe.Pointer(p)), uintptr(0), 0)
	if errno != 0 {
		return -1, errno
	}
	fd := int(r)
	// Clear CLOEXEC so the child inherits it.
	flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), uintptr(syscall.F_GETFD), 0)
	if errno != 0 {
		return -1, errno
	}
	if _, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), uintptr(syscall.F_SETFD), flags&^uintptr(mfdCloexec)); errno != 0 {
		return -1, errno
	}
	return fd, nil
}

func main() {
	modelPath, wav := os.Args[1], os.Args[2]
	f, err := os.Open(modelPath)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	fd, err := memfdCreate("ggml-model")
	if err != nil {
		panic(err)
	}
	buf := make([]byte, chunkBytes)
	var total int64
	for {
		n, err := f.Read(buf)
		if n > 0 {
			if _, werr := syscall.Write(fd, buf[:n]); werr != nil {
				panic(werr)
			}
			total += int64(n)
		}
		if err != nil {
			break
		}
	}
	// The child must be able to open the memfd AND mmap it: pass the raw fd
	// number through ExtraFiles so it lands at 3 in the child, and point the
	// loader at /proc/self/fd/3.
	clearCloexec(fd)
	path := fmt.Sprintf("/proc/self/fd/%d", fd)
	fmt.Printf("memfd fd=%d path=%s bytes=%d\n", fd, path, total)

	cmd := exec.Command(os.Args[3], append(os.Args[4:], "-m", path, "-f", wav, "-l", "zh")...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.ExtraFiles = []*os.File{os.NewFile(uintptr(fd), "model")}
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "child failed: %v\n", err)
		os.Exit(1)
	}
}

func clearCloexec(fd int) {
	flags, _, _ := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), uintptr(syscall.F_GETFD), 0)
	syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), uintptr(syscall.F_SETFD), flags&^uintptr(mfdCloexec))
}
