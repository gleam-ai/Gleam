package textmatch

import "strings"

const MinAlnumTerm = 3

func Terms(s string) map[string]bool {
	terms := map[string]bool{}
	runes := []rune(strings.ToLower(s))
	for i := 0; i+1 < len(runes); i++ {
		if isCJK(runes[i]) && isCJK(runes[i+1]) {
			terms[string(runes[i:i+2])] = true
		}
	}
	for w := range alnumWords(strings.ToLower(s)) {
		if len([]rune(w)) >= MinAlnumTerm {
			terms[w] = true
		}
	}
	return terms
}

func Score(terms map[string]bool, hay string) int {
	if len(terms) == 0 {
		return 0
	}
	hay = strings.ToLower(hay)
	words := alnumWords(hay)
	s := 0
	for term := range terms {
		if IsAlnumWord(term) {
			if words[term] {
				s++
			}
			continue
		}
		if strings.Contains(hay, term) {
			s++
		}
	}
	return s
}

func IsAlnumWord(term string) bool {
	for _, r := range term {
		return isAlnum(r)
	}
	return false
}

func alnumWords(s string) map[string]bool {
	words := map[string]bool{}
	for _, w := range strings.FieldsFunc(s, func(r rune) bool {
		return !isAlnum(r)
	}) {
		words[w] = true
	}
	return words
}

func isCJK(r rune) bool {
	return r >= 0x4E00 && r <= 0x9FFF
}

func isAlnum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
}
