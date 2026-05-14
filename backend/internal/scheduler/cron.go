package scheduler

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Cron is a parsed 5-field cron expression. Construct with Parse and
// then call Next(time) to find the next firing time.
//
// Supported syntax:
//
//	*        any value
//	5        exactly 5
//	1,3,5    list
//	1-5      inclusive range
//	*/5      step (every 5 starting from min)
//	1-10/2   range with step
//
// Not supported: @hourly, @daily, ?, L, W, # — swap in robfig/cron in
// a follow-up if these matter (see scheduler.go package docstring).
type Cron struct {
	minute, hour, dom, month, dow uint64 // bitmasks per field
}

// fieldSpec captures the legal range and a sentinel "any" value
// (used by the dom/dow merging rule below).
type fieldSpec struct {
	min, max int
}

var (
	specMinute = fieldSpec{0, 59}
	specHour   = fieldSpec{0, 23}
	specDOM    = fieldSpec{1, 31}
	specMonth  = fieldSpec{1, 12}
	specDOW    = fieldSpec{0, 6} // Sunday=0
)

// ErrInvalidCron is the sentinel error from Parse.
var ErrInvalidCron = errors.New("scheduler: invalid cron expression")

// Parse turns a 5-field cron string into a Cron. Leading and trailing
// whitespace is tolerated; internal whitespace is collapsed.
func Parse(expr string) (Cron, error) {
	fields := strings.Fields(strings.TrimSpace(expr))
	if len(fields) != 5 {
		return Cron{}, fmt.Errorf("%w: want 5 fields, got %d", ErrInvalidCron, len(fields))
	}
	specs := []fieldSpec{specMinute, specHour, specDOM, specMonth, specDOW}
	masks := make([]uint64, 5)
	for i, f := range fields {
		m, err := parseField(f, specs[i])
		if err != nil {
			return Cron{}, fmt.Errorf("%w: field %d: %v", ErrInvalidCron, i+1, err)
		}
		masks[i] = m
	}
	return Cron{minute: masks[0], hour: masks[1], dom: masks[2], month: masks[3], dow: masks[4]}, nil
}

// parseField builds the bitmask for one cron field.
func parseField(f string, spec fieldSpec) (uint64, error) {
	var out uint64
	for _, part := range strings.Split(f, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return 0, errors.New("empty list element")
		}
		step := 1
		if slashIdx := strings.Index(part, "/"); slashIdx >= 0 {
			stepStr := part[slashIdx+1:]
			s, err := strconv.Atoi(stepStr)
			if err != nil || s <= 0 {
				return 0, fmt.Errorf("bad step %q", stepStr)
			}
			step = s
			part = part[:slashIdx]
			if part == "" || part == "*" {
				part = fmt.Sprintf("%d-%d", spec.min, spec.max)
			}
		}
		var lo, hi int
		switch {
		case part == "*":
			lo, hi = spec.min, spec.max
		case strings.Contains(part, "-"):
			ab := strings.SplitN(part, "-", 2)
			a, err := strconv.Atoi(ab[0])
			if err != nil {
				return 0, fmt.Errorf("bad range low %q", ab[0])
			}
			b, err := strconv.Atoi(ab[1])
			if err != nil {
				return 0, fmt.Errorf("bad range high %q", ab[1])
			}
			lo, hi = a, b
		default:
			n, err := strconv.Atoi(part)
			if err != nil {
				return 0, fmt.Errorf("bad number %q", part)
			}
			lo, hi = n, n
		}
		if lo < spec.min || hi > spec.max || lo > hi {
			return 0, fmt.Errorf("%d-%d out of [%d, %d]", lo, hi, spec.min, spec.max)
		}
		for v := lo; v <= hi; v += step {
			out |= 1 << uint(v)
		}
	}
	if out == 0 {
		return 0, errors.New("matches no values")
	}
	return out, nil
}

// matches reports whether v is in the mask.
func (c Cron) matches(mask uint64, v int) bool {
	return mask&(1<<uint(v)) != 0
}

// Next returns the smallest t' > from where t' matches the cron
// expression. Operates in the supplied location — callers pass the
// schedule's LoadLocation() so DST-bound expressions like "0 9 * * *"
// fire at 9am local even across spring-forward / fall-back.
//
// Caps the search at 4 years to avoid infinite loops on pathological
// expressions like "0 0 30 2 *" (Feb 30, never matches).
func (c Cron) Next(from time.Time, loc *time.Location) (time.Time, error) {
	if loc == nil {
		loc = time.UTC
	}
	// Start one minute past from with seconds/nanos zeroed so we don't
	// re-fire the same minute on the next tick.
	t := from.In(loc).Add(time.Minute).Truncate(time.Minute)
	deadline := from.AddDate(4, 0, 0)
	for t.Before(deadline) {
		if !c.matches(c.month, int(t.Month())) {
			// Jump to first of next month at 00:00.
			y, m := t.Year(), t.Month()+1
			if m > 12 {
				y++
				m = 1
			}
			t = time.Date(y, m, 1, 0, 0, 0, 0, loc)
			continue
		}
		// dom / dow combine with OR (POSIX cron rule): when both are
		// restrictive, either matching is enough.
		domAny := c.dom == fullMask(specDOM)
		dowAny := c.dow == fullMask(specDOW)
		domOK := c.matches(c.dom, t.Day())
		dowOK := c.matches(c.dow, int(t.Weekday()))
		var dayOK bool
		switch {
		case domAny && dowAny:
			dayOK = true
		case domAny:
			dayOK = dowOK
		case dowAny:
			dayOK = domOK
		default:
			dayOK = domOK || dowOK
		}
		if !dayOK {
			// Next midnight in the same month — simpler than a smart skip.
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, loc)
			continue
		}
		if !c.matches(c.hour, t.Hour()) {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, loc)
			continue
		}
		if !c.matches(c.minute, t.Minute()) {
			t = t.Add(time.Minute)
			continue
		}
		return t, nil
	}
	return time.Time{}, fmt.Errorf("%w: no occurrence within 4 years of %s", ErrInvalidCron, from)
}

func fullMask(spec fieldSpec) uint64 {
	var m uint64
	for v := spec.min; v <= spec.max; v++ {
		m |= 1 << uint(v)
	}
	return m
}
