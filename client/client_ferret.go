package client

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/MontFerret/ferret/pkg/compiler"
	"github.com/MontFerret/ferret/pkg/drivers"
	"github.com/MontFerret/ferret/pkg/drivers/cdp"
	fhttp "github.com/MontFerret/ferret/pkg/drivers/http"
	"github.com/MontFerret/ferret/pkg/runtime"
)

// doRequestFerret is a simple wrapper to read response according to options.
func (c *Client) doRequestFerret(req *Request) (*Response, error) {
	// log.Printf("Client.doRequestFerret(req)\n")
	// fql := fmt.Sprintf(`RETURN DOCUMENT(%q, { driver: "cdp" })`, req.URL.String())

	fql := fmt.Sprintf(`LET doc = DOCUMENT(%q, { driver: "cdp" })
%s
RETURN doc
`,
		req.URL.String(),
		req.Meta["_fql"])

	// log.Printf("fql:\n%s", fql)

	// fmt.Printf("CallFQL(info: %q, fql, url: %q)\n", info, url)
	program, err := compiler.New().Compile(fql)
	if err != nil {
		return nil, fmt.Errorf("while compiling FQL: %w", err)
	}

	// create a root context
	ctx := context.Background()

	// enable HTML drivers
	// by default, Ferret Runtime does not know about any HTML drivers
	// all HTML manipulations are done via functions from standard library
	// that assume that at least one driver is available
	ctx = drivers.WithContext(ctx, cdp.NewDriver())
	ctx = drivers.WithContext(ctx, fhttp.NewDriver(), drivers.AsDefault())

	qbody, err := program.Run(ctx, runtime.WithLog(os.Stdout))
	//, runtime.WithParams(source.RecordMap{"url": url}))
	if err != nil {
		return nil, fmt.Errorf("while running FQL: %w", err)
	}
	sbody, err := strconv.Unquote(string(qbody))
	if err != nil {
		return nil, fmt.Errorf("while unquoting FQL result: %w", err)
	}

	body := []byte(sbody)
	// log.Printf("in Client.doRequestFerret(), len(body): %d", len(body))
	// log.Printf("in Client.doRequestFerret(), string(body)[0:1000]: %s", string(body)[0:1000])

	// // Do request
	// resp, err := c.Do(req.Request)
	// defer func() {
	// 	if resp != nil {
	// 		resp.Body.Close()
	// 	}
	// }()
	// if err != nil {
	// 	return nil, fmt.Errorf("response: %w", err)
	// }

	// // Limit response body reading
	// bodyReader := io.LimitReader(resp.Body, c.opt.MaxBodySize)

	// // Decode response
	// if resp.Request.Method != "HEAD" && resp.ContentLength > 0 {
	// 	if req.Encoding != "" {
	// 		if enc, _ := charset.Lookup(req.Encoding); enc != nil {
	// 			bodyReader = transform.NewReader(bodyReader, enc.NewDecoder())
	// 		}
	// 	} else {
	// 		if !c.opt.CharsetDetectDisabled {
	// 			contentType := req.Header.Get("Content-Type")
	// 			bodyReader, err = charset.NewReader(bodyReader, contentType)
	// 			if err != nil {
	// 				return nil, fmt.Errorf("charset detection error on content-type %s: %w", contentType, err)
	// 			}
	// 		}
	// 	}
	// }

	// body, err := io.ReadAll(bodyReader)
	// if err != nil {
	// 	return nil, fmt.Errorf("reading body: %w", err)
	// }
	h := http.Header{}
	h.Add("Content-Type", "text/html")

	response := Response{
		Response: &http.Response{
			Status:     "200 OK",
			StatusCode: 200,
			Proto:      "HTTP/1.0",
			Header:     h,
		},
		Body:    body,
		Request: req,
	}
	// log.Printf("in Client.DoRequest(req), response: %T", response)
	return &response, nil
}
