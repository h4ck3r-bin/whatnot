package engine

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"
)

type TimeWindow struct {
	StartHour   int
	StartMinute int
	EndHour     int
	EndMinute   int
	Location    *time.Location
}

func NewTimeWindow(timezone string, start string, end string) (TimeWindow, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return TimeWindow{}, err
	}
	startHour, startMinute, err := parseClock(start)
	if err != nil {
		return TimeWindow{}, fmt.Errorf("parse order window start: %w", err)
	}
	endHour, endMinute, err := parseClock(end)
	if err != nil {
		return TimeWindow{}, fmt.Errorf("parse order window end: %w", err)
	}
	return TimeWindow{
		StartHour:   startHour,
		StartMinute: startMinute,
		EndHour:     endHour,
		EndMinute:   endMinute,
		Location:    loc,
	}, nil
}

func (w TimeWindow) Contains(now time.Time) bool {
	local := now.In(w.Location)
	start := time.Date(local.Year(), local.Month(), local.Day(), w.StartHour, w.StartMinute, 0, 0, w.Location)
	end := time.Date(local.Year(), local.Month(), local.Day(), w.EndHour, w.EndMinute, 0, 0, w.Location)
	return !local.Before(start) && !local.After(end)
}

func (w TimeWindow) NextRandomRun(now time.Time) time.Time {
	local := now.In(w.Location)
	start := time.Date(local.Year(), local.Month(), local.Day(), w.StartHour, w.StartMinute, 0, 0, w.Location)
	end := time.Date(local.Year(), local.Month(), local.Day(), w.EndHour, w.EndMinute, 0, 0, w.Location)
	if local.After(end) {
		start = start.Add(24 * time.Hour)
		end = end.Add(24 * time.Hour)
	}
	if local.Before(start) {
		local = start
	}
	span := end.Sub(local)
	if span <= 0 {
		return local
	}
	return local.Add(time.Duration(rand.Int63n(int64(span))))
}

func parseClock(value string) (int, int, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected HH:MM")
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, err
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, err
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("clock value out of range")
	}
	return hour, minute, nil
}
