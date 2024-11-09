package middleware

import (
	"math/rand"
	"time"

	"github.com/findyourpaths/geziyor/client"
)

// delay delays requests
type delay struct {
	requestDelayRandomize bool
	requestDelay          time.Duration
}

func NewDelay(requestDelayRandomize bool, requestDelay time.Duration) RequestProcessor {
	if requestDelayRandomize {
		rand.Seed(time.Now().UnixNano())
	}
	return &delay{requestDelayRandomize: requestDelayRandomize, requestDelay: requestDelay}
}

func (a *delay) ProcessRequest(r *client.Request) {
	if a.requestDelayRandomize {
		min := float64(a.requestDelay) * 0.5
		max := float64(a.requestDelay) * 1.5
		// log.Printf("starting to sleep with min: %f, max: %f at %v", min, max, time.Now)
		time.Sleep(time.Duration(rand.Intn(int(max-min)) + int(min)))
		// log.Printf("finished sleeping with min: %f, max: %f at %v", min, max, time.Now)
	} else {
		time.Sleep(a.requestDelay)
	}
}
