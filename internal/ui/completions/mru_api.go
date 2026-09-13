package completions

import (
	"sync"
)

// mruGlobal is the process-wide slash MRU (loaded lazily, persisted on record).
var mruGlobal struct {
	once sync.Once
	m    *slashMRU
}

func mru() *slashMRU {
	mruGlobal.once.Do(func() { mruGlobal.m = loadMRU() })
	return mruGlobal.m
}

// RecordSlashUse remembers an accepted slash command for MRU ordering.
func RecordSlashUse(cmd string) { mru().record(cmd) }

// SortByMRU orders commands most-recently-used first (stable for never-used).
func SortByMRU(cmds []string) []string {
	m := mru()
	out := append([]string(nil), cmds...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && m.rankOf(out[j]) > m.rankOf(out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
