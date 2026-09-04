package scheduler

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// NewOnce builds a one-time task that fires at time at.
func NewOnce(at time.Time) (*Schedule, error) {
	return &Schedule{Kind: KindOnce, At: at}, nil
}

// NewRelative builds a one-time task that fires delay after now.
func NewRelative(delay time.Duration) (*Schedule, error) {
	if delay <= 0 {
		return nil, errors.New("relative delay must be positive")
	}
	return &Schedule{Kind: KindRelative, Dur: delay}, nil
}

// NewDaily builds a task that fires daily at time ("HH:MM").
func NewDaily(t string) (*Schedule, error) {
	m, err := parseTime(t)
	if err != nil {
		return nil, fmt.Errorf("daily: %w", err)
	}
	return &Schedule{Kind: KindDaily, Times: []int{m}}, nil
}

// NewWeekly builds a task that fires weekly on weekday (English weekday names)
// at time.
// weekday accepts Mon/Tue/Wed/Thu/Fri/Sat/Sun or Monday...Sunday,
// case-insensitively.
func NewWeekly(weekday, t string) (*Schedule, error) {
	d, err := parseWeekday(weekday)
	if err != nil {
		return nil, fmt.Errorf("weekly: %w", err)
	}
	m, err := parseTime(t)
	if err != nil {
		return nil, fmt.Errorf("weekly: %w", err)
	}
	return &Schedule{Kind: KindWeekly, Days: []time.Weekday{d}, Times: []int{m}}, nil
}

// NewMonthly builds a task that fires monthly on day (1-31) at time.
func NewMonthly(day int, t string) (*Schedule, error) {
	if day < 1 || day > 31 {
		return nil, fmt.Errorf("monthly: invalid day %d (1-31)", day)
	}
	m, err := parseTime(t)
	if err != nil {
		return nil, fmt.Errorf("monthly: %w", err)
	}
	return &Schedule{Kind: KindMonthly, DaysOfMonth: []int{day}, Times: []int{m}}, nil
}

// NewYearly builds a task that fires yearly on date ("MM-DD") at time.
func NewYearly(date, t string) (*Schedule, error) {
	md, err := parseMonthDay(date)
	if err != nil {
		return nil, fmt.Errorf("yearly: %w", err)
	}
	m, err := parseTime(t)
	if err != nil {
		return nil, fmt.Errorf("yearly: %w", err)
	}
	return &Schedule{Kind: KindYearly, Dates: []MonthDay{md}, Times: []int{m}}, nil
}

// ParseDateTime parses a string of the form "2006-01-02 15:04" into an instant
// in the loc timezone.
func ParseDateTime(s string, loc *time.Location) (time.Time, error) {
	s = strings.TrimSpace(s)
	layouts := []string{
		"2006-01-02 15:04",
		"2006-01-02 15:04:05",
		"2006/01/02 15:04",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid datetime %q, want 2006-01-02 15:04", s)
}

// parseTime parses "HH:MM" into minutes (0..1439).
func parseTime(t string) (int, error) {
	t = strings.TrimSpace(t)
	parts := strings.SplitN(t, ":", 2)
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid time %q, want HH:MM", t)
	}
	hh, err1 := strconv.Atoi(parts[0])
	mm, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, fmt.Errorf("invalid time %q, want HH:MM (00:00-23:59)", t)
	}
	return hh*60 + mm, nil
}

// parseWeekday parses an English weekday name into a time.Weekday.
func parseWeekday(s string) (time.Weekday, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "mon", "monday":
		return time.Monday, nil
	case "tue", "tues", "tuesday":
		return time.Tuesday, nil
	case "wed", "wednesday":
		return time.Wednesday, nil
	case "thu", "thur", "thurs", "thursday":
		return time.Thursday, nil
	case "fri", "friday":
		return time.Friday, nil
	case "sat", "saturday":
		return time.Saturday, nil
	case "sun", "sunday":
		return time.Sunday, nil
	}
	return 0, fmt.Errorf("invalid weekday %q (use Mon/Tue/Wed/Thu/Fri/Sat/Sun or Monday...Sunday)", s)
}

// parseMonthDay parses "MM-DD" into a month-day.
func parseMonthDay(s string) (MonthDay, error) {
	s = strings.TrimSpace(s)
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return MonthDay{}, fmt.Errorf("invalid date %q, want MM-DD", s)
	}
	mo, err1 := strconv.Atoi(parts[0])
	d, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || !validDate(2000, time.Month(mo), d) {
		return MonthDay{}, fmt.Errorf("invalid date %q, want MM-DD", s)
	}
	return MonthDay{Month: mo, Day: d}, nil
}

// Describe returns an ASCII description of the task's firing rules, for
// display to the agent.
func (s *Schedule) Describe() string {
	switch s.Kind {
	case KindOnce:
		return s.At.Format("2006-01-02 15:04") + " (once)"
	case KindRelative:
		return formatDuration(s.Dur) + " later (once)"
	case KindDaily:
		return "every day at " + formatMinute(s.Times[0])
	case KindWeekly:
		return fmt.Sprintf("every %s at %s", weekdayAbbr(s.Days[0]), formatMinute(s.Times[0]))
	case KindMonthly:
		return fmt.Sprintf("every month on day %d at %s", s.DaysOfMonth[0], formatMinute(s.Times[0]))
	case KindYearly:
		return fmt.Sprintf("every year on %02d-%02d at %s", s.Dates[0].Month, s.Dates[0].Day, formatMinute(s.Times[0]))
	}
	return ""
}

func formatMinute(m int) string {
	return fmt.Sprintf("%02d:%02d", m/60, m%60)
}

func weekdayAbbr(d time.Weekday) string {
	switch d {
	case time.Monday:
		return "Mon"
	case time.Tuesday:
		return "Tue"
	case time.Wednesday:
		return "Wed"
	case time.Thursday:
		return "Thu"
	case time.Friday:
		return "Fri"
	case time.Saturday:
		return "Sat"
	case time.Sunday:
		return "Sun"
	}
	return d.String()
}

func formatDuration(d time.Duration) string {
	if d%time.Minute != 0 {
		return fmt.Sprintf("%.0f seconds", d.Seconds())
	}
	if d%(24*time.Hour) == 0 {
		return fmt.Sprintf("%d days", int(d.Hours()/24))
	}
	if d%time.Hour == 0 {
		return fmt.Sprintf("%d hours", int(d.Hours()))
	}
	return fmt.Sprintf("%d minutes", int(d.Minutes()))
}

func validDate(y int, m time.Month, d int) bool {
	if m < 1 || m > 12 || d < 1 || d > 31 {
		return false
	}
	t := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return int(t.Month()) == int(m) && t.Day() == d
}
