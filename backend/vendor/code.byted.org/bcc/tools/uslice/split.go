package uslice

type OneSplit struct {
	B    int
	E    int
	Size int
}

//把size分割多份，每份batch个 //每份为[B,E)
func SplitNum(size int, batch int) (r []OneSplit) {
	if size <= 0 || batch <= 0 {
		return
	}
	if size > batch {
		r = make([]OneSplit, 0, size/batch+1)
	}
	now := 0
	for now < size {
		stop := now + batch
		if stop > size {
			stop = size
		}
		r = append(r, OneSplit{B: now, E: stop, Size: stop - now})
		now = stop
	}
	return r
}
