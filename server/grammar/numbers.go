package grammar

import (
	"crypto/sha256"
	"strconv"
	"strings"
)

func sha(b []byte) []byte { h := sha256.Sum256(b); return h[:] }

var units = map[string]int{"zero": 0, "oh": 0, "one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6, "seven": 7, "eight": 8, "nine": 9,
	"ten": 10, "eleven": 11, "twelve": 12, "thirteen": 13, "fourteen": 14, "fifteen": 15, "sixteen": 16, "seventeen": 17, "eighteen": 18, "nineteen": 19}
var tens = map[string]int{"twenty": 20, "thirty": 30, "forty": 40, "fifty": 50, "sixty": 60, "seventy": 70, "eighty": 80, "ninety": 90}

// Key is a word's matching key: lower case, and a number word as its digits, so typed
// "fifteen" and heard "15" are one symbol for matching (ledger L0002). What is rendered
// stays as written.
func Key(w string) string {
	l := strings.ToLower(w)
	if v, ok := units[l]; ok {
		return strconv.Itoa(v)
	}
	if v, ok := tens[l]; ok {
		return strconv.Itoa(v)
	}
	return l
}

// NumberValue reads a run of number words as one number: "three forty" is 340 (a unit
// then a tens word, the way prices and sizes are said), "twenty five" is 25, "three
// hundred forty" is 340, "two thousand" is 2000. ok is false when the run isn't a number.
func NumberValue(words []string) (int, bool) {
	if len(words) == 0 {
		return 0, false
	}
	total, cur, seen := 0, 0, false
	for i, w := range words {
		l := strings.ToLower(w)
		switch {
		case l == "hundred":
			if cur == 0 {
				cur = 1
			}
			cur *= 100
		case l == "thousand":
			if cur == 0 {
				cur = 1
			}
			total += cur * 1000
			cur = 0
		case units[l] != 0 || l == "zero" || l == "oh":
			v := units[l]
			if cur%10 == 0 && cur%100 != 0 && cur < 100 && v < 10 {
				cur += v // twenty five
			} else if cur > 0 && cur < 10 && v < 10 {
				return 0, false // "three five" is two numbers
			} else {
				cur = cur + v
			}
		case tens[l] != 0:
			v := tens[l]
			if cur > 0 && cur < 10 && i > 0 {
				cur = cur*100 + v // three forty
			} else if cur >= 100 && cur%100 == 0 {
				cur += v // three hundred forty
			} else if cur == 0 {
				cur = v
			} else {
				return 0, false
			}
		default:
			if n, err := strconv.Atoi(l); err == nil && len(words) == 1 {
				return n, true
			}
			return 0, false
		}
		seen = true
	}
	return total + cur, seen
}
