package arv

import (
	"strings"
	"sync"
	"time"
)

var (
	spinOnce sync.Once
	spinText string
)

// spinSrc defines spin(), about 300 ms of pure work on this machine as it
// runs now: the race detector, or load, makes the same loop several times
// slower. It is calibrated once per test binary, so the tests that time
// spin() against deadlines and sleeps keep their ratios.
func spinSrc() string {
	spinOnce.Do(func() {
		const unit = `let a = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10];
let unit = fn() { for x in a { for y in a { for z in a { for w in a { 1 } } } }; 0 };
`
		per := time.Duration(1 << 62)
		for i := 0; i < 3; i++ {
			start := time.Now()
			if _, err := Run(unit+"for i in ["+ones(10)+"] { unit() }", Options{}); err != nil {
				panic(err)
			}
			per = min(per, time.Since(start)/10)
		}
		k := min(max(int(300*time.Millisecond/max(per, time.Microsecond)), 1), 5000)
		spinText = unit + "let reps = [" + ones(k) + "];\nlet spin = fn() { for r in reps { unit() }; 0 };\n"
	})
	return spinText
}

// ones is "1, 1, ..., 1", n of them.
func ones(n int) string {
	return strings.TrimSuffix(strings.Repeat("1, ", n), ", ")
}
