package aws

import (
	"strconv"
	"strings"
)

// parseAmountCents parses a decimal money string (as Cost Explorer
// returns, e.g. "12.3456789") into integer cents, rounding to two
// decimal places. It returns ok=false for an unparseable amount so the
// caller can skip it rather than corrupt the running total. Only the
// first two fractional digits are honoured; the rest are truncated
// (Cost Explorer figures are estimates and the panel shows cents).
func parseAmountCents(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	neg := false
	if strings.HasPrefix(s, "-") {
		neg = true
		s = s[1:]
	}
	intPart, fracPart, _ := strings.Cut(s, ".")
	whole, err := strconv.ParseInt(intPart, 10, 64)
	if err != nil {
		return 0, false
	}
	// Normalise the fractional part to exactly two digits.
	frac := fracPart
	if len(frac) > 2 {
		frac = frac[:2]
	}
	for len(frac) < 2 {
		frac += "0"
	}
	var cents int64
	if frac != "" {
		c, err := strconv.ParseInt(frac, 10, 64)
		if err != nil {
			return 0, false
		}
		cents = c
	}
	total := whole*100 + cents
	if neg {
		total = -total
	}
	return total, true
}

// formatCents renders integer cents back into a "D.DD" decimal string.
func formatCents(cents int64) string {
	neg := cents < 0
	if neg {
		cents = -cents
	}
	out := strconv.FormatInt(cents/100, 10) + "." + twoDigits(cents%100)
	if neg {
		return "-" + out
	}
	return out
}

func twoDigits(n int64) string {
	if n < 10 {
		return "0" + strconv.FormatInt(n, 10)
	}
	return strconv.FormatInt(n, 10)
}
