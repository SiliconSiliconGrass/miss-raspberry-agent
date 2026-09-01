// Package scheduler 提供定时任务的核心逻辑：时间表达式解析、任务存储与触发分发。
// 时间表达式支持多种中文自然语言写法（见 Parse 的文档）。
package scheduler

import (
	"time"
)

// Kind 表示时间表达式的类型。
type Kind string

const (
	// KindOnce 是绝对时间或“今天/明天”等一次性触发。
	KindOnce Kind = "once"
	// KindRelative 是相对时间，例如“10min之后”，触发一次。
	KindRelative Kind = "relative"
	// KindDaily 是每天固定时间重复触发。
	KindDaily Kind = "daily"
	// KindWeekly 是每周若干星期几、若干时间重复触发。
	KindWeekly Kind = "weekly"
	// KindMonthly 是每月若干日、若干时间重复触发。
	KindMonthly Kind = "monthly"
	// KindYearly 是每年若干月日、若干时间重复触发。
	KindYearly Kind = "yearly"
)

// MonthDay 表示“每年 M 月 D 日”。
type MonthDay struct {
	Month int
	Day   int
}

// Schedule 是一个时间表达式的解析结果，描述任务何时触发。
type Schedule struct {
	Kind        Kind
	At          time.Time      // KindOnce 的触发时刻
	Dur         time.Duration  // KindRelative 的延迟时长
	Days        []time.Weekday // KindDaily / KindWeekly 的星期集合
	DaysOfMonth []int          // KindMonthly 的每月几号
	Dates       []MonthDay     // KindYearly 的月日集合
	Times       []int          // 每天/每周/每月/每年触发的小时分钟数（0..1439）
}

// Repeat 报告该表达式是否会重复触发。
func (s *Schedule) Repeat() bool {
	switch s.Kind {
	case KindDaily, KindWeekly, KindMonthly, KindYearly:
		return true
	default:
		return false
	}
}

// Next 返回 now 之后（严格晚于）的下一次触发时间；若不再触发则 ok=false。
// now 应已位于目标时区（例如 Asia/Shanghai），返回的时间为绝对时刻。
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
		// 下周同一天最多需要偏移 7 天。
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
		// 覆盖到下一个整月（含月末的 31 号），最多查 370 天。
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
		// 覆盖到下一次出现（2月29日最多跨 4 年），最多查 1500 天。
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

// nextAtDay 返回 day 当天在 times 中第一个严格晚于 now 的时刻。
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
