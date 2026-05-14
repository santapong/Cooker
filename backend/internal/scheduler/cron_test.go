package scheduler

import (
	"errors"
	"testing"
	"time"
)

func TestParseValid(t *testing.T) {
	cases := []string{
		"* * * * *",
		"0 * * * *",
		"*/5 * * * *",
		"0 9 * * 1-5",
		"0 0 1 * *",
		"15,45 * * * *",
		"0-30/2 * * * *",
	}
	for _, expr := range cases {
		if _, err := Parse(expr); err != nil {
			t.Errorf("Parse(%q): %v", expr, err)
		}
	}
}

func TestParseInvalid(t *testing.T) {
	cases := []string{
		"",
		"* * * *",
		"* * * * * *",
		"60 * * * *",
		"* 24 * * *",
		"* * 0 * *",
		"* * 32 * *",
		"* * * 13 *",
		"* * * * 7",
		"abc * * * *",
		"5-1 * * * *",   // bad range
		"*/0 * * * *",   // zero step
		",1 * * * *",    // empty list element
		"1, * * * *",    // trailing empty element
	}
	for _, expr := range cases {
		_, err := Parse(expr)
		if !errors.Is(err, ErrInvalidCron) {
			t.Errorf("Parse(%q) want ErrInvalidCron, got %v", expr, err)
		}
	}
}

func TestNextEveryMinute(t *testing.T) {
	c, err := Parse("* * * * *")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 5, 14, 10, 0, 30, 0, time.UTC)
	next, err := c.Next(now, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 5, 14, 10, 1, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next=%s want %s", next, want)
	}
}

func TestNextSpecificMinute(t *testing.T) {
	c, _ := Parse("15 * * * *")
	now := time.Date(2026, 5, 14, 10, 30, 0, 0, time.UTC)
	next, _ := c.Next(now, time.UTC)
	want := time.Date(2026, 5, 14, 11, 15, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next=%s want %s", next, want)
	}
}

func TestNextDailyAt9(t *testing.T) {
	c, _ := Parse("0 9 * * *")
	now := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	next, _ := c.Next(now, time.UTC)
	want := time.Date(2026, 5, 15, 9, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next=%s want %s", next, want)
	}
}

func TestNextWeekdaysAt9(t *testing.T) {
	c, _ := Parse("0 9 * * 1-5")
	// Saturday after 9am -> next Monday at 9am.
	now := time.Date(2026, 5, 16, 10, 0, 0, 0, time.UTC) // Sat
	next, _ := c.Next(now, time.UTC)
	want := time.Date(2026, 5, 18, 9, 0, 0, 0, time.UTC) // Mon
	if !next.Equal(want) {
		t.Fatalf("next=%s want %s", next, want)
	}
}

func TestNextEvery5Minutes(t *testing.T) {
	c, _ := Parse("*/5 * * * *")
	now := time.Date(2026, 5, 14, 10, 2, 0, 0, time.UTC)
	next, _ := c.Next(now, time.UTC)
	want := time.Date(2026, 5, 14, 10, 5, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next=%s want %s", next, want)
	}
}

func TestNextRespectsLocation(t *testing.T) {
	// "0 9 * * *" in New York time should fire at 9am local.
	loc, _ := time.LoadLocation("America/New_York")
	c, _ := Parse("0 9 * * *")
	// 8am NYT (= 12pm UTC roughly during DST).
	now := time.Date(2026, 7, 14, 8, 30, 0, 0, loc)
	next, err := c.Next(now, loc)
	if err != nil {
		t.Fatal(err)
	}
	if next.Hour() != 9 {
		t.Fatalf("hour=%d want 9 (in NYT)", next.Hour())
	}
	if next.Location().String() != "America/New_York" {
		t.Fatalf("location=%s want America/New_York", next.Location())
	}
}

func TestNextSkipsBadDate(t *testing.T) {
	// Feb 30 never exists — the search should bail within 4 years.
	c, _ := Parse("0 0 30 2 *")
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_, err := c.Next(now, time.UTC)
	if !errors.Is(err, ErrInvalidCron) {
		t.Fatalf("want ErrInvalidCron (no occurrence), got %v", err)
	}
}
