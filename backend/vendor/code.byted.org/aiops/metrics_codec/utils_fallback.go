//go:build noasm || !amd64
// +build noasm !amd64

package metrics_codec

// validateTagNameSIMD fallback to validateStringTable on platform not support SIMD
func validateTagNameSIMD(s string) bool {
	return validateStringTable(&nameLUT, s)
}

// validateTagValueSIMD fallback to validateStringTable on platform not support SIMD
func validateTagValueSIMD(s string) bool {
	return validateStringTable(&valueLUT, s)
}
