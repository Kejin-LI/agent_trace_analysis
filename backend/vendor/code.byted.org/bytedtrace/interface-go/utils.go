package bytedtracer

func Contains(slice []string, value string) bool {
	for i := 0; i < len(slice); i++ {
		if slice[i] == value {
			return true
		}
	}
	return false
}

func MergeSlice(source, dt []string) []string {
	for i := 0; i < len(dt); i++ {
		if !Contains(source, dt[i]) {
			source = append(source, dt[i])
		}
	}
	return source
}
