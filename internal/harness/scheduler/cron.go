package scheduler

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Cron struct {
	min, hour, dom, mon, dow fieldSpec
	domRestricted            bool
	dowRestricted            bool
}

type fieldSpec struct {
	any    bool
	values map[int]bool
}

func (f fieldSpec) match(v int) bool {
	if f.any {
		return true
	}
	return f.values[v]
}

func ParseCron(expr string) (*Cron, error) {
	fields := strings.Fields(strings.TrimSpace(expr))
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron 表达式需要 5 段（分 时 日 月 周），收到 %d 段: %q", len(fields), expr)
	}
	c := &Cron{min: fieldSpec{values: map[int]bool{}}, hour: fieldSpec{values: map[int]bool{}},
		dom: fieldSpec{values: map[int]bool{}}, mon: fieldSpec{values: map[int]bool{}}, dow: fieldSpec{values: map[int]bool{}}}
	var err error
	if c.min.any, c.min.values, err = parseField(fields[0], 0, 59); err != nil {
		return nil, fmt.Errorf("分钟字段: %w", err)
	}
	if c.hour.any, c.hour.values, err = parseField(fields[1], 0, 23); err != nil {
		return nil, fmt.Errorf("小时字段: %w", err)
	}
	if c.dom.any, c.dom.values, err = parseField(fields[2], 1, 31); err != nil {
		return nil, fmt.Errorf("日字段: %w", err)
	}
	if c.mon.any, c.mon.values, err = parseField(fields[3], 1, 12); err != nil {
		return nil, fmt.Errorf("月字段: %w", err)
	}
	if c.dow.any, c.dow.values, err = parseField(fields[4], 0, 7); err != nil {
		return nil, fmt.Errorf("周字段: %w", err)
	}
	if !c.dow.any {
		if c.dow.values[7] {
			c.dow.values[0] = true
		}
		delete(c.dow.values, 7)
	}
	c.domRestricted = !c.dom.any
	c.dowRestricted = !c.dow.any
	return c, nil
}

func parseField(s string, min, max int) (bool, map[int]bool, error) {
	values := map[int]bool{}
	any := false
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return false, nil, fmt.Errorf("空片段 %q", s)
		}
		step := 1
		if i := strings.Index(part, "/"); i >= 0 {
			stepStr := part[i+1:]
			part = part[:i]
			n, err := strconv.Atoi(stepStr)
			if err != nil || n <= 0 {
				return false, nil, fmt.Errorf("非法步长 %q", stepStr)
			}
			step = n
		}
		lo, hi := min, max
		switch part {
		case "*", "":
			if step == 1 && len(values) == 0 && !any {
				any = true
				continue
			}
		default:
			if i := strings.Index(part, "-"); i > 0 {
				a, err1 := strconv.Atoi(part[:i])
				b, err2 := strconv.Atoi(part[i+1:])
				if err1 != nil || err2 != nil {
					return false, nil, fmt.Errorf("非法区间 %q", part)
				}
				lo, hi = a, b
			} else {
				a, err := strconv.Atoi(part)
				if err != nil {
					return false, nil, fmt.Errorf("非法数值 %q", part)
				}
				lo, hi = a, a
			}
		}
		if lo < min || hi > max || lo > hi {
			return false, nil, fmt.Errorf("数值越界 [%d,%d]: %q", min, max, part)
		}
		for v := lo; v <= hi; v += step {
			values[v] = true
		}
	}
	return any, values, nil
}

func (c *Cron) dayMatch(t time.Time) bool {
	if !c.mon.match(int(t.Month())) {
		return false
	}
	domOK := c.dom.match(t.Day())
	dowOK := c.dow.match(int(t.Weekday()))
	if c.domRestricted && c.dowRestricted {
		return domOK || dowOK
	}
	return domOK && dowOK
}

func (c *Cron) Match(t time.Time) bool {
	return c.dayMatch(t) && c.hour.match(t.Hour()) && c.min.match(t.Minute())
}

func (c *Cron) Next(after time.Time) time.Time {
	t := after.Truncate(time.Minute).Add(time.Minute)
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	// Scan by day, not by minute within one year: a Feb 29 schedule can be up
	// to eight years away (2096 -> 2104, since 2100 is not a leap year), and a
	// one-year minute scan would report "never" for a valid schedule.
	const maxDays = 366*8 + 2
	for i := 0; i <= maxDays; i++ {
		d := day.AddDate(0, 0, i)
		year, month, dom := d.Date()
		if !c.dayMatch(time.Date(year, month, dom, 12, 0, 0, 0, t.Location())) {
			continue
		}
		startHour, startMin := 0, 0
		if i == 0 {
			startHour, startMin = t.Hour(), t.Minute()
		}
		for h := startHour; h < 24; h++ {
			if !c.hour.match(h) {
				continue
			}
			minStart := 0
			if i == 0 && h == startHour {
				minStart = startMin
			}
			for minute := minStart; minute < 60; minute++ {
				if c.min.match(minute) {
					return time.Date(year, month, dom, h, minute, 0, 0, t.Location())
				}
			}
		}
	}
	return time.Time{}
}
