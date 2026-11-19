//go:build windows

package cotw

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
	"syscall"
	"unsafe"

	"cotw-pop-viewer/adf"
)

// Windows process and memory access primitives used by the live reader.
//
// Everything here is read only: OpenProcess is called with PROCESS_VM_READ and
// PROCESS_QUERY_INFORMATION, and the library never writes to the game's memory.

var (
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	psapi                    = syscall.NewLazyDLL("psapi.dll")
	procOpenProcess          = kernel32.NewProc("OpenProcess")
	procReadProcessMemory    = kernel32.NewProc("ReadProcessMemory")
	procVirtualQueryEx       = kernel32.NewProc("VirtualQueryEx")
	procCloseHandle          = kernel32.NewProc("CloseHandle")
	procCreateSnapshot       = kernel32.NewProc("CreateToolhelp32Snapshot")
	procProcess32FirstW      = kernel32.NewProc("Process32FirstW")
	procProcess32NextW       = kernel32.NewProc("Process32NextW")
	procEnumProcessModules   = psapi.NewProc("EnumProcessModules")
	procGetModuleFileNameExW = psapi.NewProc("GetModuleFileNameExW")
	procGetModuleInformation = psapi.NewProc("GetModuleInformation")
)

const (
	processQueryInformation = 0x0400
	processVMRead           = 0x0010
	th32csSnapProcess       = 0x00000002

	memCommit  = 0x1000
	memPrivate = 0x20000
	memMapped  = 0x40000
	memImage   = 0x1000000

	pageNoAccess  = 0x01
	pageReadOnly  = 0x02
	pageReadWrite = 0x04
	pageWriteCopy = 0x08
	pageExecute   = 0x10
	pageExecRead  = 0x20
	pageExecRW    = 0x40
	pageExecWC    = 0x80
	pageGuard     = 0x100
)

// processEntry32 is PROCESSENTRY32W.
type processEntry32 struct {
	Size            uint32
	Usage           uint32
	ProcessID       uint32
	DefaultHeapID   uintptr
	ModuleID        uint32
	Threads         uint32
	ParentProcessID uint32
	PriClassBase    int32
	Flags           uint32
	ExeFile         [260]uint16
}

// memoryBasicInformation is MEMORY_BASIC_INFORMATION (x64).
type memoryBasicInformation struct {
	BaseAddress       uintptr
	AllocationBase    uintptr
	AllocationProtect uint32
	PartitionID       uint16
	_                 uint16
	RegionSize        uintptr
	State             uint32
	Protect           uint32
	Type              uint32
	_                 uint32
}

// gameProcessNames are the executable names the game runs under.
var gameProcessNames = []string{"theHunterCotW_F.exe", "theHunterCotW_E.exe", "theHunterCotW.exe"}

// process is an open, read-only handle on a running game.
type process struct {
	pid    uint32
	handle syscall.Handle
	// moduleBase and moduleSize bound the executable's own image, which is
	// where every virtual method table lives.
	moduleBase uintptr
	moduleSize uintptr
}

// findGamePID returns the pid of the running game, if there is one.
func findGamePID() (uint32, string, bool) {
	snap, _, _ := procCreateSnapshot.Call(th32csSnapProcess, 0)
	if snap == ^uintptr(0) {
		return 0, "", false
	}
	defer procCloseHandle.Call(snap)

	var entry processEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	ok, _, _ := procProcess32FirstW.Call(snap, uintptr(unsafe.Pointer(&entry)))
	for ok != 0 {
		name := syscall.UTF16ToString(entry.ExeFile[:])
		for _, want := range gameProcessNames {
			if strings.EqualFold(name, want) {
				return entry.ProcessID, name, true
			}
		}
		entry.Size = uint32(unsafe.Sizeof(entry))
		ok, _, _ = procProcess32NextW.Call(snap, uintptr(unsafe.Pointer(&entry)))
	}
	return 0, "", false
}

// openProcessHandle opens the game process for reading.
func openProcessHandle(pid uint32) (*process, error) {
	h, _, err := procOpenProcess.Call(processQueryInformation|processVMRead, 0, uintptr(pid))
	if h == 0 {
		return nil, fmt.Errorf("cotw: open game process %d: %w", pid, err)
	}
	p := &process{pid: pid, handle: syscall.Handle(h)}
	p.findMainModule()
	return p, nil
}

// findMainModule locates the executable image among the process modules.
func (p *process) findMainModule() {
	var mods [1024]uintptr
	var needed uint32
	procEnumProcessModules.Call(uintptr(p.handle), uintptr(unsafe.Pointer(&mods[0])),
		uintptr(unsafe.Sizeof(mods)), uintptr(unsafe.Pointer(&needed)))
	n := int(needed) / int(unsafe.Sizeof(uintptr(0)))
	for i := 0; i < n && i < len(mods); i++ {
		var name [260]uint16
		l, _, _ := procGetModuleFileNameExW.Call(uintptr(p.handle), mods[i],
			uintptr(unsafe.Pointer(&name[0])), 259)
		if l == 0 {
			continue
		}
		path := syscall.UTF16ToString(name[:l])
		base := strings.ToLower(path)
		for _, want := range gameProcessNames {
			if strings.HasSuffix(base, strings.ToLower(want)) {
				var mi struct {
					Base  uintptr
					Size  uint32
					Entry uintptr
				}
				procGetModuleInformation.Call(uintptr(p.handle), mods[i],
					uintptr(unsafe.Pointer(&mi)), unsafe.Sizeof(mi))
				p.moduleBase = mi.Base
				p.moduleSize = uintptr(mi.Size)
				return
			}
		}
	}
}

// close releases the process handle.
func (p *process) close() {
	if p.handle != 0 {
		procCloseHandle.Call(uintptr(p.handle))
		p.handle = 0
	}
}

// alive reports whether the process still exists.
func (p *process) alive() bool {
	if p.handle == 0 {
		return false
	}
	var code uint32
	r, _, _ := procGetExitCodeProcess.Call(uintptr(p.handle), uintptr(unsafe.Pointer(&code)))
	return r != 0 && code == 259 // STILL_ACTIVE
}

var procGetExitCodeProcess = kernel32.NewProc("GetExitCodeProcess")

// read copies n bytes at addr. It returns the bytes actually read; a short read
// is not an error (the tail of a region may be unmapped).
func (p *process) read(addr uintptr, buf []byte) (int, error) {
	if len(buf) == 0 {
		return 0, nil
	}
	var got uintptr
	r, _, err := procReadProcessMemory.Call(uintptr(p.handle), addr,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&got)))
	if r == 0 {
		return 0, err
	}
	return int(got), nil
}

// readExact copies exactly len(buf) bytes or reports an error.
func (p *process) readExact(addr uintptr, buf []byte) error {
	n, err := p.read(addr, buf)
	if err != nil {
		return err
	}
	if n != len(buf) {
		return fmt.Errorf("cotw: short read at %#x (%d of %d bytes)", addr, n, len(buf))
	}
	return nil
}

// regions enumerates the process's address space.
func (p *process) regions() []memoryBasicInformation {
	var out []memoryBasicInformation
	var addr uintptr
	for {
		var m memoryBasicInformation
		n, _, _ := procVirtualQueryEx.Call(uintptr(p.handle), addr,
			uintptr(unsafe.Pointer(&m)), unsafe.Sizeof(m))
		if n == 0 {
			break
		}
		out = append(out, m)
		next := m.BaseAddress + m.RegionSize
		if next <= addr {
			break
		}
		addr = next
	}
	return out
}

// readable reports whether a region can be read without faulting on a guard
// page and is backed by committed memory.
func (m memoryBasicInformation) readable() bool {
	if m.State != memCommit {
		return false
	}
	if m.Protect&(pageNoAccess|pageGuard) != 0 {
		return false
	}
	// Pure execute pages are code; data does not live there.
	if m.Protect == pageExecute {
		return false
	}
	return true
}

// heapOnly reports whether the region is private/mapped application memory, as
// opposed to the executable image itself.
func (m memoryBasicInformation) heapOnly() bool {
	return m.Type == memPrivate || m.Type == memMapped
}

// scanNeedly searches the process's heap for a byte pattern, returning the
// addresses it was found at. Whole regions are read in chunks so a 7 GB heap
// takes a few seconds, not a few minutes.
func (p *process) scanBytes(needle []byte) []uintptr {
	if len(needle) == 0 {
		return nil
	}
	var hits []uintptr
	buf := make([]byte, 8<<20+len(needle))
	for _, m := range p.regions() {
		if !m.readable() || !m.heapOnly() {
			continue
		}
		for off := uintptr(0); off < m.RegionSize; off += uintptr(len(buf) - len(needle)) {
			n := int(m.RegionSize - off)
			if n > len(buf) {
				n = len(buf)
			}
			got, err := p.read(m.BaseAddress+off, buf[:n])
			if err != nil || got < len(needle) {
				break
			}
			chunk := buf[:got]
			pos := 0
			for {
				j := bytes.Index(chunk[pos:], needle)
				if j < 0 {
					break
				}
				hits = append(hits, m.BaseAddress+off+uintptr(pos+j))
				pos += j + 1
			}
		}
	}
	return hits
}

// readAround reads n bytes starting at addr, clamped to the containing region.
func (p *process) readAround(addr uintptr, before, total int) ([]byte, uintptr) {
	start := addr
	if before > 0 {
		if addr >= uintptr(before) {
			start = addr - uintptr(before)
		} else {
			start = 0
		}
	}
	buf := make([]byte, total)
	got, err := p.read(start, buf)
	if err != nil {
		return nil, start
	}
	return buf[:got], start
}

// animalEntitySentinel marks a materialised animal entity. The engine writes
// FLT_MAX followed by 0xDEADBEEF as the terminator of the entity's transform
// block. In a loaded reserve this eight byte sequence occurs exactly once per
// animal and nowhere else, which is what makes the entities findable: a scan
// for it returns precisely the reserve's animals.
var animalEntitySentinel = []byte{0xff, 0xff, 0x7f, 0x7f, 0xef, 0xbe, 0xad, 0xde}

// Field offsets inside a materialised animal entity. The Back values are
// measured backwards from the sentinel address; the At values are the same
// fields measured forwards from the start of a window that begins at the
// position field (sentinel-entityPositionBack), which is how a record is read.
const (
	entityPositionBack = 188 // f32 X, then Y at Back-184, Z at Back-180
	entityWeightBack   = 104 // f32
	entityScoreBack    = 100 // f32
	entitySeedBack     = 84  // u32 visualVariationSeed

	entityWeightAt = entityPositionBack - entityWeightBack // 84
	entityScoreAt  = entityPositionBack - entityScoreBack  // 88
	entitySeedAt   = entityPositionBack - entitySeedBack   // 104

	// entityWindow is the number of bytes to read from the position field to
	// cover the seed.
	entityWindow = entitySeedAt + 4
)

// liveEntity is one materialised animal as the engine holds it.
type liveEntity struct {
	// addr is the sentinel address, which identifies the entity across reads.
	addr     uintptr
	seed     uint32
	weight   float32
	score    float32
	position Vec3
}

// entitiesFor returns the reserve's animal entities, reusing the addresses of a
// previous scan when they still describe the same reserve.
//
// A heap scan costs tens of seconds; re-reading a few thousand known addresses
// costs milliseconds. The addresses are therefore cached: on the next call the
// entities are simply re-read from where they were, and a fresh scan only
// happens when the reserve changed or enough of the addresses stopped holding
// an entity that the layout must have moved. Animals that vanished between
// calls (shot, or despawned) are dropped without invalidating the rest.
func (l *LiveGame) entitiesFor(reserve int) []liveEntity {
	if len(l.entityAddrs) > 0 && l.entityReserve == reserve {
		ents, missing := l.readEntitiesAt(l.entityAddrs)
		// A handful of animals leaving is normal. Losing more than a few
		// percent means the entities were reallocated, so rescan.
		limit := len(l.entityAddrs) / 50
		if limit < 4 {
			limit = 4
		}
		if missing <= limit {
			return ents
		}
	}
	l.entityAddrs = nil
	ents := l.locateEntities()
	l.entityAddrs = make([]uintptr, 0, len(ents))
	for _, e := range ents {
		l.entityAddrs = append(l.entityAddrs, e.addr)
	}
	l.entityReserve = reserve
	return ents
}

// readEntitiesAt re-reads the entities at known sentinel addresses. It returns
// the ones still present and how many were gone.
func (l *LiveGame) readEntitiesAt(addrs []uintptr) ([]liveEntity, int) {
	out := make([]liveEntity, 0, len(addrs))
	buf := make([]byte, entityWindow)
	missing := 0
	for _, addr := range addrs {
		if l.proc.readExact(addr-entityPositionBack, buf) != nil {
			missing++
			continue
		}
		e, ok := decodeEntity(buf)
		if !ok {
			missing++
			continue
		}
		e.addr = addr
		out = append(out, e)
	}
	return out, missing
}

// locateEntities finds every materialised animal entity in the game's heap and
// decodes its live fields. Records whose fields cannot be read are skipped.
func (l *LiveGame) locateEntities() []liveEntity {
	hits := l.proc.scanBytes(animalEntitySentinel)
	out := make([]liveEntity, 0, len(hits))
	buf := make([]byte, entityWindow)
	for _, hit := range hits {
		if hit < entityPositionBack {
			continue
		}
		if l.proc.readExact(hit-entityPositionBack, buf) != nil {
			continue
		}
		e, ok := decodeEntity(buf)
		if !ok {
			continue
		}
		e.addr = hit
		out = append(out, e)
	}
	return out
}

// decodeEntity reads one entity out of a window that begins at the position
// field, i.e. at sentinel-entityPositionBack. It exists so the field arithmetic
// can be tested against a synthetic record without a running game.
func decodeEntity(buf []byte) (liveEntity, bool) {
	if len(buf) < entityWindow {
		return liveEntity{}, false
	}
	f32 := func(off int) float32 {
		return math.Float32frombits(binary.LittleEndian.Uint32(buf[off:]))
	}
	return liveEntity{
		seed:     binary.LittleEndian.Uint32(buf[entitySeedAt:]),
		weight:   f32(entityWeightAt),
		score:    f32(entityScoreAt),
		position: Vec3{X: float64(f32(0)), Y: float64(f32(4)), Z: float64(f32(8))},
	}, true
}

// locateWorldState finds the game's live ReserveWorldData object by locating
// the ADF image that types it. Save-file ADF images keep their type names in a
// name table, so the type name is a stable marker that survives patches; the
// reserve number is then read from the parsed object rather than guessed from
// an offset.
func (l *LiveGame) locateWorldState() (*worldState, error) {
	for _, needle := range [][]byte{
		[]byte("ReserveWorldData"),                    // ASCII name table
		[]byte("R\x00e\x00s\x00e\x00r\x00v\x00e\x00"), // UTF-16 name table
	} {
		for _, hit := range l.proc.scanBytes(needle) {
			if ws := l.parseWorldStateAt(hit); ws != nil {
				return ws, nil
			}
		}
	}
	return nil, errors.New("cotw: reserve world state not found in memory")
}

// parseWorldStateAt looks backwards from a type-name hit for the ADF image it
// belongs to, parses the image and reads the object's Reserve field.
func (l *LiveGame) parseWorldStateAt(hit uintptr) *worldState {
	const window = 128 << 10
	buf, start := l.proc.readAround(hit, window, window+4096)
	if len(buf) < 64 {
		return nil
	}
	magic := []byte{' ', 'F', 'D', 'A'}
	idx := bytes.LastIndex(buf, magic)
	if idx < 0 || len(buf) < idx+64 {
		return nil
	}
	img := buf[idx:]
	total := int(binary.LittleEndian.Uint32(img[40:]))
	if total < 64 || total > 64<<20 {
		return nil
	}
	if total > len(img) {
		full := make([]byte, total)
		if l.proc.readExact(start+uintptr(idx), full) != nil {
			return nil
		}
		img = full
	} else {
		img = img[:total]
	}
	a, err := adf.Parse(img)
	if err != nil {
		return nil
	}
	v := a.InstanceOfTypePrefix("ReserveWorldData")
	if v == nil {
		return nil
	}
	f := v.Field("Reserve")
	if f == nil {
		return nil
	}
	n, err := f.Int64()
	if err != nil || n < 0 || n > 64 {
		return nil
	}
	return &worldState{addr: start + uintptr(idx), reserve: int(n)}
}
