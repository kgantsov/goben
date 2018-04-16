package goben

import (
	"fmt"
	"io/ioutil"
	"sync"
	"sync/atomic"
	"time"

	"github.com/valyala/fasthttp"
)

type goben struct {
	requestsLeft      int64
	requestsNumber    int
	connectionsNumber int
	url               string
	timeout           time.Duration

	requestsSucceeded uint64

	client *fasthttp.Client
	jobs   sync.WaitGroup
	done   chan bool

	RPSes     []uint64
	latencies []float64

	lock     sync.Mutex
	requests int64
	start    time.Time
}

func NewGoben(numReqs int, numConns int, url string) (*goben, error) {
	b := new(goben)
	b.requestsLeft = int64(numReqs)
	b.requestsNumber = numReqs
	b.connectionsNumber = numConns
	b.url = url
	b.done = make(chan bool)
	b.RPSes = make([]uint64, 0)
	b.latencies = make([]float64, 0)
	b.jobs.Add(b.requestsNumber)
	b.client = &fasthttp.Client{
		MaxConnsPerHost: b.connectionsNumber,
	}
	b.timeout = 2 * time.Second

	return b, nil
}

func (b *goben) makeRequest() {
	start := time.Now()

	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()

	req.Header.SetMethod("GET")
	req.SetRequestURI(b.url)

	err := b.client.DoTimeout(req, resp, b.timeout)
	if err != nil {
	}
	resp.WriteTo(ioutil.Discard)

	atomic.AddInt64(&b.requests, 1)
	b.latencies = append(b.latencies, float64(time.Since(start).Nanoseconds())/1000)

	fasthttp.ReleaseRequest(req)
	fasthttp.ReleaseResponse(resp)
}

func (b *goben) rateMeter() {
	tick := time.Tick(100 * time.Millisecond)
	for {
		select {
		case <-tick:
			b.calculateRPS()
			continue
		case <-b.done:
			b.calculateRPS()
			b.done <- true
			return
		}
	}
}

func (b *goben) calculateRPS() {
	b.lock.Lock()

	duration := time.Since(b.start)
	requests := b.requests
	b.requests = 0
	b.start = time.Now()

	b.lock.Unlock()

	rps := uint64(float64(requests) / duration.Seconds())

	if rps >= 1 {
		b.RPSes = append(b.RPSes, rps)
	}
}

func (b *goben) printRPSResults() {
	count := len(b.RPSes)
	sum := uint64(0)
	max := uint64(0)
	min := uint64(0)
	for index, val := range b.RPSes {
		sum += val
		if index == 0 {
			min = val
			max = val
		} else {
			if val < min {
				min = val
			}
			if val > max {
				max = val
			}
		}
	}
	fmt.Printf("%12v %12v %12v %12v\n", "Statistics", "Avg", "Min", "Max")
	fmt.Printf("%12v %12v %12v %12v\n", "Reqs/sec", float64(sum/uint64(count)), min, max)
}

func (b *goben) printLatenciesResults() {
	count := len(b.latencies)
	sum := float64(0)
	max := float64(0)
	min := float64(0)
	for index, val := range b.latencies {
		sum += val
		if index == 0 {
			min = val
			max = val
		} else {
			if val < min {
				min = val
			}
			if val > max {
				max = val
			}
		}
	}
	fmt.Printf(
		"%12v %12v %12v %12v\n",
		"Latencies",
		fmt.Sprintf("%.2fms", float64(sum/float64(count))/1000),
		fmt.Sprintf("%.2fms", min/1000),
		fmt.Sprintf("%.2fms", max/1000),
	)
}

func (b *goben) getJob() bool {
	requestsLeft := atomic.AddInt64(&b.requestsLeft, -1)
	return requestsLeft >= 0
}

func (b *goben) worker() {
	for b.getJob() {
		b.makeRequest()
		b.JobDone()
	}
}

func (b *goben) JobDone() {
	atomic.AddUint64(&b.requestsSucceeded, 1)
	b.jobs.Done()
}

func (b *goben) Run() {
	for i := 0; i < b.connectionsNumber; i++ {
		go b.worker()
	}

	go b.rateMeter()
	b.jobs.Wait()
	b.done <- true
	<-b.done

	b.printRPSResults()
	b.printLatenciesResults()
}
