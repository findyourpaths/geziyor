package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/findyourpaths/geziyor/cache"
	"github.com/findyourpaths/geziyor/internal"

	// scraper "github.com/shynome/go-cloudflare-scraper"
	"golang.org/x/net/html/charset"
	"golang.org/x/text/transform"
)

var (
	// ErrNoCookieJar is the error type for missing cookie jar
	ErrNoCookieJar = errors.New("cookie jar is not available")
)

// Client is a small wrapper around *http.Client to provide new methods.
type Client struct {
	*http.Client
	opt   *Options
	Cache cache.Cache
}

// Options is custom http.client options
type Options struct {
	MaxBodySize           int64
	CharsetDetectDisabled bool
	RetryTimes            int
	RetryHTTPCodes        []int
	RemoteAllocatorURL    string
	AllocatorOptions      []chromedp.ExecAllocatorOption
	ProxyFunc             func(*http.Request) (*url.URL, error)
	// Changing this will override the existing default PreActions for Rendered requests.
	// Geziyor Response will be nearly empty. Because we have no way to extract response without default pre actions.
	// So, if you set this, you should handle all navigation, header setting, and response handling yourself.
	// See defaultPreActions variable for the existing defaults.
	PreActions []chromedp.Action
}

// Default values for client
const (
	DefaultUserAgent        = "Geziyor 1.0"
	DefaultMaxBody    int64 = 1024 * 1024 * 1024 // 1GB
	DefaultRetryTimes       = 2
)

var (
	DefaultRetryHTTPCodes = []int{500, 502, 503, 504, 522, 524, 408}
)

// NewClient creates http.Client with modified values for typical web scraper
func NewClient(opt *Options) *Client {
	// Default proxy function is http.ProxyFunction
	var proxyFunction = http.ProxyFromEnvironment
	if opt.ProxyFunc != nil {
		proxyFunction = opt.ProxyFunc
	}

	httpClient := &http.Client{
		Transport: &http.Transport{
			Proxy: proxyFunction,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
				DualStack: true,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          0,    // Default: 100
			MaxIdleConnsPerHost:   1000, // Default: 2
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
		Timeout: time.Second * 180, // Google's timeout
	}

	// scraper, err := scraper.NewTransport(httpClient.Transport)
	// if err != nil {
	// 	log.Fatal(err)
	// }
	// httpClient.Transport = scraper

	client := Client{
		Client: httpClient,
		opt:    opt,
	}

	return &client
}

// DoRequest selects appropriate request handler, client or Chrome
func (c *Client) DoRequest(creq *Request) (*Response, error) {
	// log.Printf("DoRequest(creq)")
	// defer log.Printf("DoRequest(creq) returning")

	if c.Cache != nil {
		hresp, err := cache.CachedResponse(c.Cache, creq.Request)
		if hresp != nil && err == nil {
			return c.makeResponse(creq, hresp)
		}
	}

	// log.Printf("client.Client.DoRequest(req: %#v)", req)
	// log.Printf("c.Jar before: %#v", c.Jar)

	cresp, err := c.doCache(creq, func() (*Response, error) {
		// log.Printf("in DoRequest() c.doRequest*(req)")
		if creq.Rendered {
			return c.doRequestChrome(creq)
		} else if creq.Ferreted {
			return c.doRequestFerret(creq)
		}
		return c.doRequestClient(creq)
	})
	// log.Printf("in DoRequescreq c.doRequest*(req) returned")
	// log.Printf("c.Jar after: %#v", c.Jar)

	// Retry on Error
	if err != nil {
		if creq.retryCounter < c.opt.RetryTimes {
			creq.retryCounter++
			internal.Logger.Println("Retrying:", creq.URL.String())
			return c.DoRequest(creq)
		}
		return cresp, err
	}

	// Retry on http status codes
	if internal.ContainsInt(c.opt.RetryHTTPCodes, cresp.StatusCode) {
		if creq.retryCounter < c.opt.RetryTimes {
			creq.retryCounter++
			internal.Logger.Println("Retrying:", creq.URL.String(), cresp.StatusCode)
			return c.DoRequest(creq)
		}
	}

	return cresp, nil
}

// Custom type that holds your function
type myTransport struct {
	requestFn func() (*Response, error)
	response  *Response
}

// Implement the RoundTrip method for myTransport
func (t *myTransport) RoundTrip(hreq *http.Request) (*http.Response, error) {
	cresp, err := t.requestFn()
	if err != nil {
		return nil, err
	}
	t.response = cresp
	return cresp.Response, nil
}

// doCache is a simple wrapper to read response from cache.
func (c *Client) doCache(creq *Request, reqFn func() (*Response, error)) (*Response, error) {
	// log.Printf("client.Client.doCache(creq: %#v, reqFn)", creq)
	if c.Cache == nil {
		return reqFn()
	}

	t := cache.NewTransport(c.Cache)
	myt := &myTransport{requestFn: reqFn}
	t.Transport = myt
	t.Policy = cache.Dummy
	hresp, err := t.RoundTrip(creq.Request)
	// log.Printf("in client.Client.doCache(creq, reqFn), hresp: %#v", hresp)
	// log.Printf("in client.Client.doCache(creq, reqFn), err: %#v", err)
	if err != nil {
		return nil, err
	}
	if myt.response != nil {
		return myt.response, nil
	}
	defer func() {
		if hresp != nil && hresp.Body != nil {
			hresp.Body.Close()
		}
	}()
	return c.makeResponse(creq, hresp)
}

// doRequestClient is a simple wrapper to read response according to options.
func (c *Client) doRequestClient(creq *Request) (*Response, error) {
	// Do request
	hresp, err := c.Do(creq.Request)
	defer func() {
		if hresp != nil {
			hresp.Body.Close()
		}
	}()
	if err != nil {
		return nil, fmt.Errorf("response: %w", err)
	}

	return c.makeResponse(creq, hresp)
}

// makeResponse returns a client.Response from a client.Request and
// http.Response, which may have originated either from a request or a cache
// hit.
func (c *Client) makeResponse(creq *Request, hresp *http.Response) (*Response, error) {
	// Limit response body reading
	bodyReader := io.LimitReader(hresp.Body, c.opt.MaxBodySize)

	// Decode response
	if hresp.Request != nil && hresp.Request.Method != "HEAD" && hresp.ContentLength > 0 {
		if creq.Encoding != "" {
			if enc, _ := charset.Lookup(creq.Encoding); enc != nil {
				bodyReader = transform.NewReader(bodyReader, enc.NewDecoder())
			}
		} else {
			if !c.opt.CharsetDetectDisabled {
				contentType := creq.Header.Get("Content-Type")
				var err error
				bodyReader, err = charset.NewReader(bodyReader, contentType)
				if err != nil {
					return nil, fmt.Errorf("charset detection error on content-type %s: %w", contentType, err)
				}
			}
		}
	}

	body, err := io.ReadAll(bodyReader)
	if err != nil {
		return nil, fmt.Errorf("reading body: %w", err)
	}

	response := Response{
		Response: hresp,
		Body:     body,
		Request:  creq,
	}

	return &response, nil
}

// doRequestChrome opens up a new chrome instance and makes request
func (c *Client) doRequestChrome(req *Request) (*Response, error) {
	// log.Printf("client.Client.doRequestChrome(req: %#v)", req)
	log.Printf("client.Client.doRequestChrome(req.URL: %q)", req.URL)
	// Set remote allocator or use local chrome instance
	var allocCtx context.Context
	var allocCancel context.CancelFunc
	if c.opt.RemoteAllocatorURL != "" {
		allocCtx, allocCancel = chromedp.NewRemoteAllocator(context.Background(), c.opt.RemoteAllocatorURL)
	} else {
		allocCtx, allocCancel = chromedp.NewExecAllocator(context.Background(), c.opt.AllocatorOptions...)
	}
	defer allocCancel()

	// Task context
	taskCtx, taskCancel := chromedp.NewContext(allocCtx)
	defer taskCancel()

	// Initiate default pre actions
	var body string
	var res *network.Response
	var defaultPreActions = []chromedp.Action{
		network.Enable(),
		network.SetExtraHTTPHeaders(ConvertHeaderToMap(req.Header)),
		chromedp.ActionFunc(func(ctx context.Context) error {
			chromedp.ListenTarget(ctx, func(ev interface{}) {
				if event, ok := ev.(*network.EventResponseReceived); ok {
					if res == nil && event.Type == "Document" {
						res = event.Response
					}
				}
			})
			return nil
		}),
		chromedp.Navigate(req.URL.String()),
		chromedp.WaitReady(":root"),
		chromedp.ActionFunc(func(ctx context.Context) error {
			node, err := dom.GetDocument().Do(ctx)
			if err != nil {
				return err
			}
			body, err = dom.GetOuterHTML().WithNodeID(node.NodeID).Do(ctx)
			return err
		}),
	}

	// If options has pre actions, we override the default existing one.
	if len(c.opt.PreActions) != 0 {
		defaultPreActions = c.opt.PreActions
	}

	// Append custom actions to default ones.
	defaultPreActions = append(defaultPreActions, req.Actions...)

	// Run all actions
	if err := chromedp.Run(taskCtx, defaultPreActions...); err != nil {
		return nil, fmt.Errorf("request getting rendered: %w", err)
	}

	httpResponse := &http.Response{
		Request: req.Request,
	}

	// log.Printf("req.Header: %#v", req.Header)
	// log.Printf("res.RequestHeaders: %#v", res.RequestHeaders)

	// If response is set by default pre actions
	if res != nil {
		req.Header = ConvertMapToHeader(res.RequestHeaders)
		req.URL, _ = url.Parse(res.URL)
		httpResponse.StatusCode = int(res.Status)
		httpResponse.Proto = res.Protocol
		httpResponse.Header = ConvertMapToHeader(res.Headers)
	}
	httpResponse.Body = io.NopCloser(strings.NewReader(body))

	response := Response{
		Response: httpResponse,
		Body:     []byte(body),
		Request:  req,
	}

	log.Printf("client.Client.doRequestChrome(req.URL: %q), len(body): %d", req.URL, len(body))
	// log.Printf("in client.Client.doRequestChrome(req), len(body): %d", len(body))
	return &response, nil
}

// SetCookies handles the receipt of the cookies in a reply for the given URL
func (c *Client) SetCookies(URL string, cookies []*http.Cookie) error {
	if c.Jar == nil {
		return ErrNoCookieJar
	}
	u, err := url.Parse(URL)
	if err != nil {
		return err
	}
	c.Jar.SetCookies(u, cookies)
	return nil
}

// Cookies returns the cookies to send in a request for the given URL.
func (c *Client) Cookies(URL string) []*http.Cookie {
	if c.Jar == nil {
		return nil
	}
	parsedURL, err := url.Parse(URL)
	if err != nil {
		return nil
	}
	return c.Jar.Cookies(parsedURL)
}

// SetDefaultHeader sets header if not exists before
func SetDefaultHeader(header http.Header, key string, value string) http.Header {
	if header.Get(key) == "" {
		header.Set(key, value)
	}
	return header
}

// ConvertHeaderToMap converts http.Header to map[string]interface{}
func ConvertHeaderToMap(header http.Header) map[string]interface{} {
	m := make(map[string]interface{})
	for key, values := range header {
		for _, value := range values {
			m[key] = value
		}
	}
	return m
}

// ConvertMapToHeader converts map[string]interface{} to http.Header
func ConvertMapToHeader(m map[string]interface{}) http.Header {
	header := http.Header{}
	for k, v := range m {
		header.Set(k, v.(string))
	}
	return header
}

// NewRedirectionHandler returns maximum allowed redirection function with provided maxRedirect
func NewRedirectionHandler(maxRedirect int) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirect {
			return fmt.Errorf("stopped after %d redirects", maxRedirect)
		}
		return nil
	}
}
