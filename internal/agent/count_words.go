package agent

import (
	"fmt"
	"strings"
	"unicode"
)

// MinCommentWords is the exclusive lower bound: a comment must contain
// strictly more than this many words to be evaluated.
const MinCommentWords = 100

// CountWords counts words in a way that works for both spaced languages
// (English) and unspaced ideographic languages (Chinese):
//
//   - each whitespace-separated token containing Latin letters or digits
//     contributes one word;
//   - each Han (CJK) character contributes one word, since Chinese does not
//     separate words with spaces.
//
// Tokens made only of punctuation or symbols count as zero.
func CountWords(s string) int {
	count := 0
	for _, field := range strings.Fields(s) {
		cjk := 0
		hasWordRune := false
		for _, r := range field {
			switch {
			case isHan(r):
				cjk++
			case unicode.IsLetter(r) || unicode.IsDigit(r):
				hasWordRune = true
			}
		}
		count += cjk
		if hasWordRune {
			count++
		}
	}
	return count
}

func isHan(r rune) bool {
	return (r >= '\u4E00' && r <= '\u9FFF') || // CJK Unified Ideographs
		(r >= '\u3400' && r <= '\u4DBF') || // CJK Extension A
		(r >= '\uF900' && r <= '\uFAFF') // CJK Compatibility Ideographs
}

func validateCommentLength(comment string) error {
	n := CountWords(comment)
	if n <= MinCommentWords {
		return fmt.Errorf("comment must contain more than %d words, got %d", MinCommentWords, n)
	}
	return nil
}
