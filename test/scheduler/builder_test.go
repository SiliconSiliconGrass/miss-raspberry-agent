package scheduler_test

import (
	"testing"
	"time"

	"miss-raspberry-agent/internal/scheduler"
)

var bj = time.FixedZone("Asia/Shanghai", 8*3600)

func TestNewDaily(t *testing.T) {
	sch, err := scheduler.NewDaily("09:00")
	if err != nil {
		t.Fatal(err)
	}
	if sch.Kind != scheduler.KindDaily || len(sch.Times) != 1 || sch.Times[0] != 540 {
		t.Fatalf("unexpected daily schedule: %+v", sch)
	}
	if !sch.Repeat() {
		t.Fatal("daily should repeat")
	}

	for _, tm := range []string{"", "25:00", "8点", "12:60", "8"} {
		if _, err := scheduler.NewDaily(tm); err == nil {
			t.Errorf("NewDaily(%q) should fail", tm)
		}
	}
}

func TestNewWeekly(t *testing.T) {
	sch, err := scheduler.NewWeekly("Sat", "12:00")
	if err != nil {
		t.Fatal(err)
	}
	if sch.Kind != scheduler.KindWeekly {
		t.Fatalf("kind = %s, want weekly", sch.Kind)
	}
	if len(sch.Days) != 1 || sch.Days[0] != time.Saturday {
		t.Fatalf("days = %v, want [Saturday]", sch.Days)
	}
	if len(sch.Times) != 1 || sch.Times[0] != 720 {
		t.Fatalf("times = %v, want [720]", sch.Times)
	}

	// Any case, abbreviation, or full name is accepted.
	for _, in := range []string{"sat", "SAT", "Saturday"} {
		sch, err := scheduler.NewWeekly(in, "12:00")
		if err != nil || sch.Days[0] != time.Saturday {
			t.Errorf("NewWeekly(%q) = %v, %v; want Saturday", in, sch.Days, err)
		}
	}
	monday, err := scheduler.NewWeekly("Monday", "08:00")
	if err != nil {
		t.Fatal(err)
	}
	if monday.Days[0] != time.Monday {
		t.Fatalf("Monday should map to Monday, got %v", monday.Days)
	}
	sunday, err := scheduler.NewWeekly("sun", "08:00")
	if err != nil {
		t.Fatal(err)
	}
	if sunday.Days[0] != time.Sunday {
		t.Fatalf("sun should map to Sunday, got %v", sunday.Days)
	}

	for _, wd := range []string{"", "1", "周", "Someday", "Mon 8:00"} {
		if _, err := scheduler.NewWeekly(wd, "08:00"); err == nil {
			t.Errorf("NewWeekly(weekday=%q) should fail", wd)
		}
	}
	if _, err := scheduler.NewWeekly("Mon", ""); err == nil {
		t.Error("NewWeekly without time should fail")
	}
}

func TestNewMonthly(t *testing.T) {
	sch, err := scheduler.NewMonthly(30, "09:00")
	if err != nil {
		t.Fatal(err)
	}
	if len(sch.DaysOfMonth) != 1 || sch.DaysOfMonth[0] != 30 {
		t.Fatalf("days = %v, want [30]", sch.DaysOfMonth)
	}
	if len(sch.Times) != 1 || sch.Times[0] != 540 {
		t.Fatalf("times = %v, want [540]", sch.Times)
	}

	for _, d := range []int{0, 32, -1} {
		if _, err := scheduler.NewMonthly(d, "08:00"); err == nil {
			t.Errorf("NewMonthly(day=%d) should fail", d)
		}
	}
	if _, err := scheduler.NewMonthly(1, ""); err == nil {
		t.Error("NewMonthly without time should fail")
	}
}

func TestNewYearly(t *testing.T) {
	sch, err := scheduler.NewYearly("04-24", "08:00")
	if err != nil {
		t.Fatal(err)
	}
	if len(sch.Dates) != 1 || sch.Dates[0] != (scheduler.MonthDay{Month: 4, Day: 24}) {
		t.Fatalf("dates = %+v, want [04-24]", sch.Dates)
	}
	if len(sch.Times) != 1 || sch.Times[0] != 480 {
		t.Fatalf("times = %v, want [480]", sch.Times)
	}

	// February 29 is valid in 2000 (a leap year); zero padding is not required, "4-24" is also accepted.
	if _, err := scheduler.NewYearly("02-29", "00:00"); err != nil {
		t.Fatalf("02-29 should be valid: %v", err)
	}
	if _, err := scheduler.NewYearly("4-24", "08:00"); err != nil {
		t.Fatalf("4-24 should be valid: %v", err)
	}

	for _, date := range []string{"", "13-01", "04-32", "04/24", "0424", "abc"} {
		if _, err := scheduler.NewYearly(date, "08:00"); err == nil {
			t.Errorf("NewYearly(date=%q) should fail", date)
		}
	}
}

func TestNewRelative(t *testing.T) {
	sch, err := scheduler.NewRelative(10 * time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if sch.Kind != scheduler.KindRelative || sch.Dur != 10*time.Minute || sch.Repeat() {
		t.Fatalf("unexpected relative schedule: %+v", sch)
	}
	for _, d := range []time.Duration{0, -time.Minute} {
		if _, err := scheduler.NewRelative(d); err == nil {
			t.Errorf("NewRelative(%v) should fail", d)
		}
	}
}

func TestParseDateTime(t *testing.T) {
	cases := []struct {
		in   string
		want time.Time
	}{
		{"2026-09-01 10:00", time.Date(2026, 9, 1, 10, 0, 0, 0, bj)},
		{"2026-09-01 10:00:00", time.Date(2026, 9, 1, 10, 0, 0, 0, bj)},
		{"2026/09/01 10:00", time.Date(2026, 9, 1, 10, 0, 0, 0, bj)},
		{"2026-09-01", time.Date(2026, 9, 1, 0, 0, 0, 0, bj)}, // date only means that day at 00:00
	}
	for _, tc := range cases {
		got, err := scheduler.ParseDateTime(tc.in, bj)
		if err != nil {
			t.Fatalf("ParseDateTime(%q): %v", tc.in, err)
		}
		if !got.Equal(tc.want) {
			t.Fatalf("ParseDateTime(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
	for _, s := range []string{"", "2026/09/01", "10:00", "随便"} {
		if _, err := scheduler.ParseDateTime(s, bj); err == nil {
			t.Errorf("ParseDateTime(%q) should fail", s)
		}
	}
}

func TestDescribe(t *testing.T) {
	cases := []struct {
		sch  *scheduler.Schedule
		want string
	}{
		{mustSchedule(t, func() (*scheduler.Schedule, error) { return scheduler.NewWeekly("Sat", "12:00") }), "every Sat at 12:00"},
		{mustSchedule(t, func() (*scheduler.Schedule, error) { return scheduler.NewDaily("08:00") }), "every day at 08:00"},
		{mustSchedule(t, func() (*scheduler.Schedule, error) { return scheduler.NewMonthly(30, "09:00") }), "every month on day 30 at 09:00"},
		{mustSchedule(t, func() (*scheduler.Schedule, error) { return scheduler.NewYearly("04-24", "08:00") }), "every year on 04-24 at 08:00"},
		{mustSchedule(t, func() (*scheduler.Schedule, error) { return scheduler.NewRelative(90 * time.Minute) }), "90 minutes later (once)"},
		{mustSchedule(t, func() (*scheduler.Schedule, error) { return scheduler.NewOnce(time.Date(2026, 9, 1, 10, 0, 0, 0, bj)) }), "2026-09-01 10:00 (once)"},
	}
	for _, tc := range cases {
		if got := tc.sch.Describe(); got != tc.want {
			t.Errorf("Describe() = %q, want %q", got, tc.want)
		}
	}
}

func mustSchedule(t *testing.T, f func() (*scheduler.Schedule, error)) *scheduler.Schedule {
	t.Helper()
	sch, err := f()
	if err != nil {
		t.Fatal(err)
	}
	return sch
}

func TestScheduleNext(t *testing.T) {
	cases := []struct {
		name string
		sch  *scheduler.Schedule
		now  time.Time
		want time.Time
		ok   bool
	}{
		{
			name: "weekly same day later",
			sch:  mustSchedule(t, func() (*scheduler.Schedule, error) { return scheduler.NewWeekly("Sun", "08:00") }),
			now:  time.Date(2024, 1, 7, 7, 0, 0, 0, bj), // 2024-01-07 is a Sunday
			want: time.Date(2024, 1, 7, 8, 0, 0, 0, bj),
			ok:   true,
		},
		{
			name: "weekly next week",
			sch:  mustSchedule(t, func() (*scheduler.Schedule, error) { return scheduler.NewWeekly("Sun", "08:00") }),
			now:  time.Date(2024, 1, 7, 9, 0, 0, 0, bj),
			want: time.Date(2024, 1, 14, 8, 0, 0, 0, bj),
			ok:   true,
		},
		{
			name: "weekly next weekday",
			sch:  mustSchedule(t, func() (*scheduler.Schedule, error) { return scheduler.NewWeekly("Sat", "12:00") }),
			now:  time.Date(2024, 1, 7, 12, 5, 0, 0, bj), // Sunday
			want: time.Date(2024, 1, 13, 12, 0, 0, 0, bj),
			ok:   true,
		},
		{
			name: "daily tomorrow",
			sch:  mustSchedule(t, func() (*scheduler.Schedule, error) { return scheduler.NewDaily("08:00") }),
			now:  time.Date(2024, 1, 7, 9, 0, 0, 0, bj),
			want: time.Date(2024, 1, 8, 8, 0, 0, 0, bj),
			ok:   true,
		},
		{
			name: "monthly skips short month",
			sch:  mustSchedule(t, func() (*scheduler.Schedule, error) { return scheduler.NewMonthly(30, "00:00") }),
			now:  time.Date(2024, 1, 30, 10, 0, 0, 0, bj), // 2024-02 has no 30th
			want: time.Date(2024, 3, 30, 0, 0, 0, 0, bj),
			ok:   true,
		},
		{
			name: "monthly day 31",
			sch:  mustSchedule(t, func() (*scheduler.Schedule, error) { return scheduler.NewMonthly(31, "00:00") }),
			now:  time.Date(2024, 1, 31, 10, 0, 0, 0, bj),
			want: time.Date(2024, 3, 31, 0, 0, 0, 0, bj),
			ok:   true,
		},
		{
			name: "yearly next year",
			sch:  mustSchedule(t, func() (*scheduler.Schedule, error) { return scheduler.NewYearly("04-24", "00:00") }),
			now:  time.Date(2024, 4, 24, 9, 0, 0, 0, bj),
			want: time.Date(2025, 4, 24, 0, 0, 0, 0, bj),
			ok:   true,
		},
		{
			name: "yearly leap day",
			sch:  mustSchedule(t, func() (*scheduler.Schedule, error) { return scheduler.NewYearly("02-29", "00:00") }),
			now:  time.Date(2025, 2, 28, 10, 0, 0, 0, bj),
			want: time.Date(2028, 2, 29, 0, 0, 0, 0, bj),
			ok:   true,
		},
		{
			name: "absolute future",
			sch:  mustSchedule(t, func() (*scheduler.Schedule, error) { return scheduler.NewOnce(time.Date(2026, 9, 1, 10, 0, 0, 0, bj)) }),
			now:  time.Date(2026, 1, 1, 0, 0, 0, 0, bj),
			want: time.Date(2026, 9, 1, 10, 0, 0, 0, bj),
			ok:   true,
		},
		{
			name: "absolute past",
			sch:  mustSchedule(t, func() (*scheduler.Schedule, error) { return scheduler.NewOnce(time.Date(2026, 9, 1, 10, 0, 0, 0, bj)) }),
			now:  time.Date(2026, 10, 1, 0, 0, 0, 0, bj),
			ok:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := tc.sch.Next(tc.now)
			if ok != tc.ok {
				t.Fatalf("Next(%v) ok = %v, want %v", tc.now, ok, tc.ok)
			}
			if tc.ok && !got.Equal(tc.want) {
				t.Fatalf("Next(%v) = %v, want %v", tc.now, got, tc.want)
			}
		})
	}
}

func TestRelativeNext(t *testing.T) {
	sch, err := scheduler.NewRelative(10 * time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2024, 1, 7, 8, 0, 0, 0, bj)
	got, ok := sch.Next(now)
	if !ok || !got.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("Next = %v %v, want %v", got, ok, now.Add(10*time.Minute))
	}
}
