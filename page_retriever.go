package geziyor

import (
	"fmt"
	"sync"

	"github.com/PuerkitoBio/goquery"
	"github.com/findyourpaths/geziyor/client"
)

type PageRetriever interface {
	Retrieve(u string) (*goquery.Document, error)
	RetrieveFerret(u string, f string) (*goquery.Document, error)
	RetrieveRendered(u string) (*goquery.Document, error)
	// CachedPage(string) *goquery.Document
	Close()
}

// GeziyorPageRetriever wraps geziyor in a synchronous interface using channels.
type GeziyorPageRetriever struct {
	geziyor  *Geziyor
	requests chan *geziyorRequest
	results  chan *geziyorResult
	wg       sync.WaitGroup
	// cacheFn  func(string) *goquery.Document
}

// geziyorRequest represents a geziyorRequest to be processed by geziyor.
type geziyorRequest struct {
	url         string
	requestType geziyorRequestType
	ferretCode  string
	// Add any other request-specific data here
}

// A NodeType is the type of a Node.
type geziyorRequestType uint32

const (
	StandardRequestType geziyorRequestType = iota
	RenderedRequestType
	FerretRequestType
)

// geziyorResult represents the geziyorResult of a geziyor request.
type geziyorResult struct {
	response *client.Response
	err      error
	// Add any other result-specific data here
}

// NewGeziyorPageRetriever creates a new GeziyorPageRetriever instance.
// func NewGeziyorPageRetriever(g *Geziyor, cacheFn func(string) *goquery.Document) *GeziyorPageRetriever {
func NewGeziyorPageRetriever(g *Geziyor) *GeziyorPageRetriever {
	r := &GeziyorPageRetriever{
		geziyor:  g,
		requests: make(chan *geziyorRequest),
		results:  make(chan *geziyorResult),
		// cacheFn:  cacheFn,
	}

	r.wg.Add(1)
	go r.worker()
	return r
}

// func (gpr *GeziyorPageRetriever) CachedPage(u string) *goquery.Document {
// 	if gpr.cacheFn == nil {
// 		return nil
// 	}
// 	return gpr.cacheFn(u)
// }

// worker is the goroutine that processes requests.
func (gpr *GeziyorPageRetriever) worker() {
	defer gpr.wg.Done()
	for req := range gpr.requests {
		// if gqdoc := gpr.CachedPage(req.url); gqdoc != nil {
		// gpr.results <- &geziyorResult{response: &client.Response{HTMLDoc: gqdoc}}
		// continue
		// }

		switch req.requestType {
		case StandardRequestType:
			// s.NewTask("default", func(g *Geziyor) {
			gpr.geziyor.Get(req.url, func(g *Geziyor, r *client.Response) {
				gpr.results <- &geziyorResult{response: r}
			})
			// }).Run()
		case RenderedRequestType:
			// s.geziyor.NewTask("default", func(g *Geziyor) {
			gpr.geziyor.GetRendered(req.url, func(g *Geziyor, r *client.Response) {
				gpr.results <- &geziyorResult{response: r}
			})
			// }).Run()
		case FerretRequestType:
			// s.geziyor.NewTask("default", func(g *Geziyor) {
			gpr.geziyor.GetFerreted(req.url, func(g *Geziyor, r *client.Response) {
				gpr.results <- &geziyorResult{response: r}
			}, req.ferretCode)
			// }).Run()

		// s.geziyor.NewTask("default", func(g *Geziyor) {
		// 	resp, err := g.Request(client.NewRequest().SetURL(req.url))
		default:
			fmt.Printf("WARNING: ignoring unknown geziyor request type: %v\n", req.requestType)
		}
	}
}

func (s *GeziyorPageRetriever) Retrieve(u string) (*goquery.Document, error) {
	// requestURL sends a request to geziyor and waits for the result.
	// func  requestURL(url string) (*client.Response, error) {
	s.requests <- &geziyorRequest{requestType: StandardRequestType, url: u}
	res := <-s.results
	return res.response.HTMLDoc, res.err
}

func (s *GeziyorPageRetriever) RetrieveRendered(u string) (*goquery.Document, error) {
	// requestURL sends a request to geziyor and waits for the result.
	// func  requestURL(url string) (*client.Response, error) {
	s.requests <- &geziyorRequest{requestType: RenderedRequestType, url: u}
	res := <-s.results
	return res.response.HTMLDoc, res.err
}

func (s *GeziyorPageRetriever) RetrieveFerret(u string, code string) (*goquery.Document, error) {
	// requestURL sends a request to geziyor and waits for the result.
	// func  requestURL(url string) (*client.Response, error) {
	s.requests <- &geziyorRequest{requestType: FerretRequestType, url: u, ferretCode: code}
	res := <-s.results
	return res.response.HTMLDoc, res.err
}

// Close closes the GeziyorPageRetriever instance.
func (s *GeziyorPageRetriever) Close() {
	close(s.requests)
	s.wg.Wait()
	close(s.results)
}
