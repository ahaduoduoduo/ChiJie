package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

type proxyRedirectError struct {
	err error
}

func (e *proxyRedirectError) Error() string {
	return e.err.Error()
}

func (e *proxyRedirectError) Unwrap() error {
	return e.err
}

type proxyRedirect struct {
	StatusCode int    `json:"status_code"`
	FromMethod string `json:"from_method"`
	ToMethod   string `json:"to_method"`
	FromURL    string `json:"from_url"`
	ToURL      string `json:"to_url"`
	Location   string `json:"location,omitempty"`
}

type proxyRedirectTracker struct {
	server       *Server
	follow       bool
	maxRedirects int
	redirects    []proxyRedirect
	limitReached bool
}

func (s *Server) newProxyRedirectTracker(req *ProxyRequest) *proxyRedirectTracker {
	return &proxyRedirectTracker{
		server:       s,
		follow:       req != nil && req.FollowRedirects,
		maxRedirects: s.proxyMaxRedirects(),
	}
}

func (t *proxyRedirectTracker) CheckRedirect(next *http.Request, via []*http.Request) error {
	if t == nil || !t.follow {
		return http.ErrUseLastResponse
	}
	if t.maxRedirects <= 0 {
		t.maxRedirects = DefaultProxyMaxRedirects
	}
	if len(via) > t.maxRedirects {
		t.limitReached = true
		return http.ErrUseLastResponse
	}
	if next == nil || next.URL == nil {
		return http.ErrUseLastResponse
	}
	if t.server != nil {
		if err := t.server.validateTargetURL(next.Context(), next.URL.String()); err != nil {
			return &proxyRedirectError{err: err}
		}
	}

	step := proxyRedirect{
		ToMethod: next.Method,
		ToURL:    next.URL.String(),
	}
	if len(via) > 0 {
		prev := via[len(via)-1]
		if prev != nil {
			step.FromMethod = prev.Method
			if prev.URL != nil {
				step.FromURL = prev.URL.String()
			}
		}
	}
	if next.Response != nil {
		step.StatusCode = next.Response.StatusCode
		step.Location = next.Response.Header.Get("Location")
	}
	t.redirects = append(t.redirects, step)
	return nil
}

func isProxyRedirectError(err error) bool {
	var redirectErr *proxyRedirectError
	return errors.As(err, &redirectErr)
}

func (t *proxyRedirectTracker) Apply(proxyResp *proxyResponse, httpResp *http.Response) {
	if t == nil || proxyResp == nil || !t.follow {
		return
	}
	proxyResp.RedirectDetails = true
	proxyResp.Redirects = append([]proxyRedirect(nil), t.redirects...)
	proxyResp.RedirectMax = t.maxRedirects
	proxyResp.RedirectLimitReached = t.limitReached
	if httpResp != nil && httpResp.Request != nil && httpResp.Request.URL != nil {
		proxyResp.FinalURL = httpResp.Request.URL.String()
	}
}

func writeRedirectHeaders(w http.ResponseWriter, resp *proxyResponse) {
	if resp == nil || !resp.RedirectDetails {
		return
	}
	header := w.Header()
	if resp.FinalURL != "" {
		header.Set(chijieFinalURLHeader, resp.FinalURL)
	}
	header.Set(chijieRedirectCountHeader, strconv.Itoa(len(resp.Redirects)))
	if resp.RedirectMax > 0 {
		header.Set(chijieMaxRedirectsHeader, strconv.Itoa(resp.RedirectMax))
	}
	if resp.RedirectLimitReached {
		header.Set(chijieRedirectLimitReachedHeader, "true")
	}
	data, err := json.Marshal(resp.Redirects)
	if err == nil {
		header.Set(chijieRedirectsHeader, string(data))
	}
}

func applyRedirectHeadersFromRemote(proxyResp *proxyResponse, header http.Header) {
	if proxyResp == nil || header == nil {
		return
	}
	finalURL := header.Get(chijieFinalURLHeader)
	countText := header.Get(chijieRedirectCountHeader)
	redirectsText := header.Get(chijieRedirectsHeader)
	if finalURL == "" && countText == "" && redirectsText == "" {
		return
	}
	proxyResp.RedirectDetails = true
	proxyResp.FinalURL = finalURL
	proxyResp.RedirectLimitReached = strings.EqualFold(header.Get(chijieRedirectLimitReachedHeader), "true")
	if maxRedirects, err := strconv.Atoi(header.Get(chijieMaxRedirectsHeader)); err == nil {
		proxyResp.RedirectMax = maxRedirects
	}
	if redirectsText != "" {
		var redirects []proxyRedirect
		if err := json.Unmarshal([]byte(redirectsText), &redirects); err == nil {
			proxyResp.Redirects = redirects
		}
	}
}
