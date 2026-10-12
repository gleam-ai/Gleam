package llm

import "unicode/utf8"

const CacheFriendlyMinChars = 1200

func SplitCacheBoundary(a, b string) (stable, restA, restB string) {
	n := 0
	for n < len(a) && n < len(b) {
		ra, sa := utf8.DecodeRuneInString(a[n:])
		rb, _ := utf8.DecodeRuneInString(b[n:])
		if ra != rb {
			break
		}
		n += sa
	}
	return a[:n], a[n:], b[n:]
}

func StablePrefixLen(a, b string) int {
	stable, _, _ := SplitCacheBoundary(a, b)
	return utf8.RuneCountInString(stable)
}
