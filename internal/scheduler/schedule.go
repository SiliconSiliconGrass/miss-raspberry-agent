// Package scheduler provides the core logic for scheduled tasks: parsing time
// expressions, storing tasks, and dispatching triggers.
// Time expressions accept various Chinese natural-language phrasings such as
// "today/tomorrow", "in 10min", and "February 29" (see the Parse documentation).
package scheduler

import (
	"time"
)

// Kind is the type of a time expression.
type Kind string

const (
	// KindOnce is a one-time trigger at an absolute time or for Chinese
	// natural-language phrases such as "today"/"tomorrow".
	KindOnce Kind = "once"
	// KindRelative is a relative-time trigger such as the Chinese
	// natural-language phrase "in 10min", firing once.
	KindRelative Kind = "relative"
	// KindDaily repeats at a fixed time every day.
	KindDaily Kind = "daily"
	// KindWeekly repeats at one or more times on one or more weekdays each week.
	KindWeekly Kind = "weekly"
	// KindMonthly repeats at one or more times on one or more days each month.
	KindMonthly Kind = "monthly"
	// KindYearly repeats at one or more times on one or more month-day dates each year.
	KindYearly Kind = "yearly"
)

// MonthDay represents "month M, day D" of a year.
type MonthDay struct {
	Month int
	Day   int
}

// Schedule is the result of parsing a time expression and describes when a
// task fires.
type Schedule struct {
	Kind        Kind
	At          time.Time      // trigger instant for KindOnce
	Dur         time.Duration  // delay duration for KindRelative
	Days        []time.Weekday // weekdays for KindDaily / KindWeekly
	DaysOfMonth []int          // days of the month for KindMonthly
	Dates       []MonthDay     // month-day dates for KindYearly
	Times       []int          // hour-minute offsets (0..1439) of the daily/weekly/monthly/yearly firings
}

// Repeat reports whether the expression fires repeatedly.
func (s *Schedule) Repeat() bool {
	switch s.Kind {
	case KindDaily, KindWeekly, KindMonthly, KindYearly:
		return true
	default:
		return false
	}
}

// Next returns the next firing time strictly after now; ok is false if it will
// never fire again.
// now must already be in the target timezone (e.g. Asia/Shanghai); the returned
// time is an absolute instant.
func (s *Schedule) Next(now time.Time) (time.Time, bool) {
	switch s.Kind {
	case KindOnce:
		if now.Before(s.At) {
			return s.At, true
		}
		return time.Time{}, false
	case KindRelative:
		return now.Add(s.Dur), true
	case KindDaily, KindWeekly:
		// The same weekday in the next week needs an offset of at most 7 days.
		for off := 0; off < 8; off++ {
			day := now.AddDate(0, 0, off)
			if s.Kind == KindWeekly && !containsWeekday(s.Days, day.Weekday()) {
				continue
			}
			if t, ok := nextAtDay(day, s.Times, now); ok {
				return t, true
			}
		}
	case KindMonthly:
		// Cover up to the next full month (including the 31st at month end),
		// searching at most 370 days.
		for off := 0; off < 370; off++ {
			day := now.AddDate(0, 0, off)
			if !containsInt(s.DaysOfMonth, day.Day()) {
				continue
			}
			if t, ok := nextAtDay(day, s.Times, now); ok {
				return t, true
			}
		}
	case KindYearly:
		// Cover until the next occurrence (February 29 may span up to 4
		// years), searching at most 1500 days.
		for off := 0; off < 1500; off++ {
			day := now.AddDate(0, 0, off)
			if !containsMonthDay(s.Dates, int(day.Month()), day.Day()) {
				continue
			}
			if t, ok := nextAtDay(day, s.Times, now); ok {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

// nextAtDay returns the first moment on day whose time is in times and is
// strictly after now.
func nextAtDay(day time.Time, times []int, now time.Time) (time.Time, bool) {
	for _, m := range times {
		t := time.Date(day.Year(), day.Month(), day.Day(), m/60, m%60, 0, 0, day.Location())
		if t.After(now) {
			return t, true
		}
	}
	return time.Time{}, false
}

func containsWeekday(days []time.Weekday, d time.Weekday) bool {
	for _, v := range days {
		if v == d {
			return true
		}
	}
	return false
}

func containsInt(values []int, v int) bool {
	for _, x := range values {
		if x == v {
			return true
		}
	}
	return false
}

func containsMonthDay(dates []MonthDay, month, day int) bool {
	for _, d := range dates {
		if d.Month == month && d.Day == day {
			return true
		}
	}
	return false
}
