package yiigo

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net"
	"net/http"
	"path/filepath"
	"time"
)

// defaultHTTPTimeout default http request timeout
const defaultHTTPTimeout = 10 * time.Second

// httpSettings http request settings
type httpSettings struct {
	headers map[string]string
	cookies []*http.Cookie
	close   bool
	timeout time.Duration
}

// HTTPOption configures how we set up the http request.
type HTTPOption func(s *httpSettings)

// WithHTTPHeader specifies the header to http request.
func WithHTTPHeader(key, value string) HTTPOption {
	return func(s *httpSettings) {
		s.headers[key] = value
	}
}

// WithHTTPCookies specifies the cookies to http request.
func WithHTTPCookies(cookies ...*http.Cookie) HTTPOption {
	return func(s *httpSettings) {
		s.cookies = cookies
	}
}

// WithHTTPClose specifies close the connection after
// replying to this request (for servers) or after sending this
// request and reading its response (for clients).
func WithHTTPClose() HTTPOption {
	return func(s *httpSettings) {
		s.close = true
	}
}

// WithHTTPTimeout specifies the timeout to http request.
func WithHTTPTimeout(timeout time.Duration) HTTPOption {
	return func(s *httpSettings) {
		s.timeout = timeout
	}
}

type uploadmethod int

const (
	uploadbybyptes uploadmethod = iota
	uploadbypath
	uploadbyurl
)

// UploadForm is the interface for http upload
type UploadForm interface {
	// Write writes fields to multipart writer
	Write(ctx context.Context, w *multipart.Writer) error
}

type httpUpload struct {
	filefield   string
	filename    string
	method      uploadmethod
	filefrom    string
	filecontent []byte
	metafield   string
	metadata    string
}

func (u *httpUpload) Write(ctx context.Context, w *multipart.Writer) error {
	part, err := w.CreateFormFile(u.filefield, u.filename)

	if err != nil {
		return err
	}

	switch u.method {
	case uploadbypath:
		if err = u.getContentByPath(); err != nil {
			return err
		}
	case uploadbyurl:
		if err = u.getContentByURL(ctx); err != nil {
			return err
		}
	}

	if _, err = part.Write(u.filecontent); err != nil {
		return err
	}

	// metadata
	if len(u.metafield) != 0 {
		if err = w.WriteField(u.metafield, u.metadata); err != nil {
			return err
		}
	}

	return nil
}

func (u *httpUpload) getContentByPath() error {
	path, err := filepath.Abs(u.filefrom)

	if err != nil {
		return err
	}

	u.filecontent, err = ioutil.ReadFile(path)

	return err
}

func (u *httpUpload) getContentByURL(ctx context.Context) (err error) {
	u.filecontent, err = HTTPGet(ctx, u.filefrom)

	return
}

// UploadOption configures how we set up the upload from.
type UploadOption func(u *httpUpload)

// UploadByBytes uploads by file content
func UploadByBytes(content []byte) UploadOption {
	return func(u *httpUpload) {
		u.method = uploadbybyptes
		u.filecontent = content
	}
}

// UploadByPath uploads by file path
func UploadByPath(path string) UploadOption {
	return func(u *httpUpload) {
		u.method = uploadbypath
		u.filefrom = filepath.Clean(path)
	}
}

// UploadByURL uploads file by resource url
func UploadByURL(url string) UploadOption {
	return func(u *httpUpload) {
		u.method = uploadbyurl
		u.filefrom = url
	}
}

// WithMetaField specifies the metadata field to upload from.
func WithMetaField(name, value string) UploadOption {
	return func(u *httpUpload) {
		u.metafield = name
		u.metadata = value
	}
}

// NewUploadForm returns an upload form
func NewUploadForm(fieldname, filename string, options ...UploadOption) UploadForm {
	form := &httpUpload{
		filefield: fieldname,
		filename:  filename,
	}

	for _, f := range options {
		f(form)
	}

	return form
}

// HTTPClient is the interface for an http client.
type HTTPClient interface {
	// Do sends an HTTP request and returns an HTTP response.
	Do(ctx context.Context, req *http.Request, options ...HTTPOption) ([]byte, error)

	// Get sends an HTTP get request
	Get(ctx context.Context, url string, options ...HTTPOption) ([]byte, error)

	// Post sends an HTTP post request
	Post(ctx context.Context, url string, body []byte, options ...HTTPOption) ([]byte, error)

	// Upload sends an HTTP post request for uploading media
	Upload(ctx context.Context, url string, form UploadForm, options ...HTTPOption) ([]byte, error)
}

type httpclient struct {
	client  *http.Client
	timeout time.Duration
}

func (c *httpclient) Do(ctx context.Context, req *http.Request, options ...HTTPOption) ([]byte, error) {
	settings := &httpSettings{timeout: c.timeout}

	if len(options) != 0 {
		settings.headers = make(map[string]string)

		for _, f := range options {
			f(settings)
		}
	}

	// headers
	if len(settings.headers) != 0 {
		for k, v := range settings.headers {
			req.Header.Set(k, v)
		}
	}

	// cookies
	if len(settings.cookies) != 0 {
		for _, v := range settings.cookies {
			req.AddCookie(v)
		}
	}

	if settings.close {
		req.Close = true
	}

	// timeout
	ctx, cancel := context.WithTimeout(ctx, settings.timeout)

	defer cancel()

	resp, err := c.client.Do(req.WithContext(ctx))

	if err != nil {
		// If the context has been canceled, the context's error is probably more useful.
		select {
		case <-ctx.Done():
			err = ctx.Err()
		default:
		}

		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(ioutil.Discard, resp.Body)

		return nil, fmt.Errorf("error http code: %d", resp.StatusCode)
	}

	b, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		return nil, err
	}

	return b, nil
}

func (c *httpclient) Get(ctx context.Context, url string, options ...HTTPOption) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)

	if err != nil {
		return nil, err
	}

	return c.Do(ctx, req, options...)
}

func (c *httpclient) Post(ctx context.Context, url string, body []byte, options ...HTTPOption) ([]byte, error) {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))

	if err != nil {
		return nil, err
	}

	return c.Do(ctx, req, options...)
}

func (c *httpclient) Upload(ctx context.Context, url string, form UploadForm, options ...HTTPOption) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 4<<10)) // 4kb
	w := multipart.NewWriter(buf)

	if err := form.Write(ctx, w); err != nil {
		return nil, err
	}

	options = append(options, WithHTTPHeader("Content-Type", w.FormDataContentType()))

	// Don't forget to close the multipart writer.
	// If you don't close it, your request will be missing the terminating boundary.
	w.Close()

	req, err := http.NewRequest(http.MethodPost, url, buf)

	if err != nil {
		return nil, err
	}

	return c.Do(ctx, req, options...)
}

// NewHTTPClient returns a new http client
func NewHTTPClient(client *http.Client, defaultTimeout ...time.Duration) HTTPClient {
	c := &httpclient{
		client:  client,
		timeout: defaultHTTPTimeout,
	}

	if len(defaultTimeout) != 0 {
		c.timeout = defaultTimeout[0]
	}

	return c
}

// defaultHTTPClient default http client
var defaultHTTPClient = NewHTTPClient(&http.Client{
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 60 * time.Second,
		}).DialContext,
		MaxIdleConns:          0,
		MaxIdleConnsPerHost:   1000,
		MaxConnsPerHost:       1000,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	},
}, defaultHTTPTimeout)

// HTTPGet http get request
func HTTPGet(ctx context.Context, url string, options ...HTTPOption) ([]byte, error) {
	return defaultHTTPClient.Get(ctx, url, options...)
}

// HTTPPost http post request
func HTTPPost(ctx context.Context, url string, body []byte, options ...HTTPOption) ([]byte, error) {
	return defaultHTTPClient.Post(ctx, url, body, options...)
}

// HTTPUpload http upload file
func HTTPUpload(ctx context.Context, url string, form UploadForm, options ...HTTPOption) ([]byte, error) {
	return defaultHTTPClient.Upload(ctx, url, form, options...)
}
