// memfdelf: prove a whole engine ELF can be exec'd out of an anonymous memfd.
//
// Why it matters: this is the ZERO-CGO shape for embedded voice input. gopi
// stays a pure-Go CGO_ENABLED=0 binary (Rule 12 untouched); both the engine
// executable and the model image are //go:embed'ed; at run time they are
// written into anonymous memfds and the kernel execs /proc/self/fd/N. No file
// ever appears on disk, nothing to scp separately.
package main

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const (
	sysMemfdCreate = 319 // x86_64
	mfdCloexec     = 0x0001
	copyChunk      = 1 << 20
)

func memfdFromPath(path, name string) (int, int64, error) {
	src, err := os.Open(path)
	if err != nil {
		return -1, 0, err
	}
	defer src.Close()
	p, err := syscall.BytePtrFromString(name)
	if err != nil {
		return -1, 0, err
	}
	r, _, errno := syscall.Syscall(sysMemfdCreate, uintptr(unsafe.Pointer(p)), 0, 0)
	if errno != 0 {
		return -1, 0, errno
	}
	fd := int(r)
	buf := make([]byte, copyChunk)
	var total int64
	for {
		n, rerr := src.Read(buf)
		if n > 0 {
			if _, werr := syscall.Write(fd, buf[:n]); werr != nil {
				return -1, 0, werr
			}
			total += int64(n)
		}
		if rerr != nil {
			if rerr.Error() == "EOF" {
				break
			}
			break
		}
	}
	// clear CLOEXEC: the exec'd image must inherit the fd
	flags, _, _ := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), uintptr(syscall.F_GETFD), 0)
	syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), uintptr(syscall.F_SETFD), flags&^uintptr(mfdCloexec))
	return fd, total, nil
}

func main() {
	elfPath, modelPath, wav := os.Args[1], os.Args[2], os.Args[3]
	elfFD, elfBytes, err := memfdFromPath(elfPath, "engine-elf")
	if err != nil {
		panic(err)
	}
	mdlFD, mdlBytes, err := memfdFromPath(modelPath, "ggml-model")
	if err != nil {
		panic(err)
	}
	elfLink := fmt.Sprintf("/proc/self/fd/%d", elfFD)
	mdlLink := fmt.Sprintf("/proc/self/fd/%d", mdlFD)
	fmt.Printf("memfd elf=%s bytes=%d\nmemfd model=%s bytes=%d\n", elfLink, elfBytes, mdlLink, mdlBytes)
	fmt.Printf("on-disk check: ls of the temp dir must show NO engine/model file\n")

	// ExtraFiles puts elf at child fd 3 and model at child fd 4.
	attr := &os.ProcAttr{
		Files: []*os.File{
			os.Stdin, os.Stdout, os.Stderr,
			os.NewFile(uintptr(elfFD), "engine-elf"),
			os.NewFile(uintptr(mdlFD), "ggml-model"),
		},
	}
	// argv[0] must be the fd path so the kernel reads the ELF from the memfd.
	argv := []string{elfLink, "-m", "/proc/self/fd/4", "-f", wav, "-l", "zh", "-t", "2", "-np"}
	pid, err := os.StartProcess(elfLink, argv, attr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "StartProcess(%s): %v\n", elfLink, err)
		os.Exit(1)
	}
	st, err := pid.Wait()
	if err != nil {
		panic(err)
	}
	fmt.Printf("child exit=%d\n", st.ExitCode())
}
