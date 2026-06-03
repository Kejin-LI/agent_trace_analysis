package logs

import (
	"strings"
	"sync/atomic"
	osTime "time"
	"unsafe"
)

var (
	currentTime *time
	clock       = osTime.Millisecond * 10
	ticker      = osTime.NewTicker(clock)
	zone        []byte
)

func SetClock(t osTime.Duration) {
	atomic.StoreInt64((*int64)(&clock), int64(t))
}

type time struct {
	osTime.Time
	serialBytes []byte
}

func current() time {
	return *(*time)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&currentTime))))
}

func refreshTask() {
	localC := atomic.LoadInt64((*int64)(&clock))
	for {
		cur := <-ticker.C
		refreshCurrentTime(cur)
		clock := atomic.LoadInt64((*int64)(&clock))
		if clock != localC {
			ticker.Stop()
			ticker = osTime.NewTicker(osTime.Duration(clock))
		}
	}
}

func refreshCurrentTime(cur osTime.Time) {
	curT := time{
		Time:        cur,
		serialBytes: make([]byte, 0, 28),
	}
	curT.serialBytes = timeDate(cur, curT.serialBytes)
	atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&currentTime)), unsafe.Pointer(&curT))
}

func init() {
	timeStr := strings.Split(osTime.Now().String(), " ") // 2021-07-15 12:01:15.501308 +0800 CST m=+0.005035907
	if len(timeStr) >= 3 {
		zone = []byte(timeStr[2])
	}
	refreshCurrentTime(osTime.Now())
	go refreshTask()
}

const (
	zeroAscii = '0'
)

func timeDate(t osTime.Time, c []byte) []byte {
	year, month, day := t.Date()
	//year
	c = append(c, byte(year/1000)+zeroAscii)
	c = append(c, byte(year%1000/100)+zeroAscii)
	c = append(c, byte(year%100/10)+zeroAscii)
	c = append(c, byte(year%10)+zeroAscii)
	c = append(c, '-')
	//month
	c = append(c, byte(month/10)+zeroAscii)
	c = append(c, byte(month%10)+zeroAscii)
	c = append(c, '-')
	//day
	c = append(c, byte(day/10)+zeroAscii)
	c = append(c, byte(day%10)+zeroAscii)
	c = append(c, ' ')
	hour, minute, second := t.Clock()
	//hour
	c = append(c, byte(hour/10)+zeroAscii)
	c = append(c, byte(hour%10)+zeroAscii)
	c = append(c, ':')
	//minute
	c = append(c, byte(minute/10)+zeroAscii)
	c = append(c, byte(minute%10)+zeroAscii)
	c = append(c, ':')
	//second
	c = append(c, byte(second/10)+zeroAscii)
	c = append(c, byte(second%10)+zeroAscii)
	c = append(c, ',')
	ms := t.Nanosecond() / 1e6
	c = append(c, byte(ms/100)+zeroAscii)
	c = append(c, byte(ms%100/10)+zeroAscii)
	c = append(c, byte(ms%10)+zeroAscii)
	// zone
	if len(zone) != 0 {
		c = append(c, zone...)
	}

	return c
}
