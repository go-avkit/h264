package decoder

// neighboursForRef is the generic form the optimised neighboursFor replaced:
// four neighbourMotion calls, each locating its block from luma coordinates.
func (d *sliceDecoder) neighboursForRef(list, x, y, w int) (a, b, c motion) {
	curZ := zscanOf[y>>2][x>>2]
	a, okA := d.neighbourMotion(list, x-1, y, curZ)
	b, okB := d.neighbourMotion(list, x, y-1, curZ)
	c, okC := d.neighbourMotion(list, x+w, y-1, curZ)
	if !okC {
		c, okC = d.neighbourMotion(list, x-1, y-1, curZ)
	}
	if !okB && !okC && okA {
		b, c = a, a
	}
	return a, b, c
}
