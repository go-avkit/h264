package mc

import "unsafe"

// shares reports whether the backing arrays of a and b overlap, judged on the
// whole slices (the conservative reading of the kernels' own check, which
// refuses a destination block that intersects the source footprint). Today a
// prediction never reads the picture it writes (field and MBAFF pictures are
// refused); the fields of one frame will share a buffer, and the kernels panic
// on that, so such a call takes the portable path.
func shares(a, b []byte) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	a0 := uintptr(unsafe.Pointer(&a[0]))
	b0 := uintptr(unsafe.Pointer(&b[0]))
	return a0 < b0+uintptr(len(b)) && b0 < a0+uintptr(len(a))
}
