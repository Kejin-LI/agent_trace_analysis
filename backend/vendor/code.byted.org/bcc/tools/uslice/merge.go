package uslice

func StringSliceMerge(slList ...[]string) []string {
	elemM := make(map[string]struct{})
	for _, sl := range slList {
		for _, elem := range sl {
			elemM[elem] = struct{}{}
		}
	}
	ret := make([]string, 0)
	for k := range elemM {
		ret = append(ret, k)
	}
	return ret
}
