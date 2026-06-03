package logs

import (
	"strings"
	"time"
)

const (
	zeroAscii = '0'
)

var zone [5]byte // +0100

func init() {
	timeStr := strings.Split(time.Now().String(), " ") // 2021-07-15 12:01:15.501308 +0800 CST m=+0.005035907
	if len(timeStr) >= 3 {
		copy(zone[:], timeStr[2])
	}
}

func timeDate(t time.Time, includeZone bool) []byte {
	var b [28]byte
	year, month, day := t.Date()
	//year
	b[0] = byte(year/1000) + zeroAscii
	b[1] = byte(year%1000/100) + zeroAscii
	b[2] = byte(year%100/10) + zeroAscii
	b[3] = byte(year%10) + zeroAscii
	b[4] = '-'
	//month
	b[5] = byte(month/10) + zeroAscii
	b[6] = byte(month%10) + zeroAscii
	b[7] = '-'
	//day
	b[8] = byte(day/10) + zeroAscii
	b[9] = byte(day%10) + zeroAscii
	b[10] = ' '
	hour, minute, second := t.Clock()
	//hour
	b[11] = byte(hour/10) + zeroAscii
	b[12] = byte(hour%10) + zeroAscii
	b[13] = ':'
	//minute
	b[14] = byte(minute/10) + zeroAscii
	b[15] = byte(minute%10) + zeroAscii
	b[16] = ':'
	//second
	b[17] = byte(second/10) + zeroAscii
	b[18] = byte(second%10) + zeroAscii
	b[19] = ','
	ms := t.Nanosecond() / 1e6
	b[20] = byte(ms/100) + zeroAscii
	b[21] = byte(ms%100/10) + zeroAscii
	b[22] = byte(ms%10) + zeroAscii
	// zone
	if includeZone && len(zone) != 0 {
		for i := range zone {
			b[23+i] = zone[i]
		}

		return b[:]
	}

	return b[:23]
}
