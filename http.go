package yiigo

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// httpSetting http request setting
type httpSetting struct {
	headers map[string]string
	cookies []*http.Cookie
	close   bool
}

// HTTPOption configures how we set up the http request.
type HTTPOption func(s *httpSetting)

// WithHTTPHeader specifies the header to http request.
func WithHTTPHeader(key, value string) HTTPOption {
	return func(s *httpSetting) {
		s.headers[key] = value
	}
}

// WithHTTPCookies specifies the cookies to http request.
func WithHTTPCookies(cookies ...*http.Cookie) HTTPOption {
	return func(s *httpSetting) {
		s.cookies = cookies
	}
}

// WithHTTPClose specifies close the connection after
// replying to this request (for servers) or after sending this
// request and reading its response (for clients).
func WithHTTPClose() HTTPOption {
	return func(s *httpSetting) {
		s.close = true
	}
}

// UploadForm is the interface for http upload
type UploadForm interface {
	// Write writes fields to multipart writer
	Write(w *multipart.Writer) error
}

type fileField struct {
	fieldname string
	filename  string
	body      []byte
}

type uploadform struct {
	filefield []*fileField
	formfield map[string]string
}

func (f *uploadform) Write(w *multipart.Writer) error {
	if len(f.filefield) == 0 {
		return errors.New("empty file field")
	}

	for _, field := range f.filefield {
		part, err := w.CreateFormFile(field.fieldname, field.filename)

		if err != nil {
			return err
		}

		if _, err = part.Write(field.body); err != nil {
			return err
		}
	}

	for name, value := range f.formfield {
		if err := w.WriteField(name, value); err != nil {
			return err
		}
	}

	return nil
}

// UploadField configures how we set up the upload from.
type UploadField func(f *uploadform)

// WithFileField specifies the file field to upload from.
func WithFileField(fieldname, filename string, body []byte) UploadField {
	return func(f *uploadform) {
		f.filefield = append(f.filefield, &fileField{
			fieldname: fieldname,
			filename:  filename,
			body:      body,
		})
	}
}

// WithFormField specifies the form field to upload from.
func WithFormField(fieldname, fieldvalue string) UploadField {
	return func(u *uploadform) {
		u.formfield[fieldname] = fieldvalue
	}
}

// NewUploadForm returns an upload form
func NewUploadForm(fields ...UploadField) UploadForm {
	form := &uploadform{
		filefield: make([]*fileField, 0),
		formfield: make(map[string]string),
	}

	for _, f := range fields {
		f(form)
	}

	return form
}

// HTTPClient is the interface for a http client.
type HTTPClient interface {
	// Do sends an HTTP request and returns an HTTP response.
	// Should use context to specify the timeout for request.
	Do(ctx context.Context, method, reqURL string, body io.Reader, options ...HTTPOption) (*http.Response, error)

	// Upload issues a UPLOAD to the specified URL.
	// Should use context to specify the timeout for request.
	Upload(ctx context.Context, reqURL string, form UploadForm, options ...HTTPOption) (*http.Response, error)
}

type httpclient struct {
	client *http.Client
}

func (c *httpclient) Do(ctx context.Context, method, reqURL string, body io.Reader, options ...HTTPOption) (*http.Response, error) {
	req, err := http.NewRequest(method, reqURL, body)

	if err != nil {
		return nil, err
	}

	setting := new(httpSetting)

	if len(options) != 0 {
		setting.headers = make(map[string]string)

		for _, f := range options {
			f(setting)
		}
	}

	// headers
	if len(setting.headers) != 0 {
		for k, v := range setting.headers {
			req.Header.Set(k, v)
		}
	}

	// cookies
	if len(setting.cookies) != 0 {
		for _, v := range setting.cookies {
			req.AddCookie(v)
		}
	}

	if setting.close {
		req.Close = true
	}

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

	return resp, err
}

func (c *httpclient) Upload(ctx context.Context, reqURL string, form UploadForm, options ...HTTPOption) (*http.Response, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 4<<10)) // 4kb
	w := multipart.NewWriter(buf)

	if err := form.Write(w); err != nil {
		return nil, err
	}

	options = append(options, WithHTTPHeader("Content-Type", w.FormDataContentType()))

	// Don't forget to close the multipart writer.
	// If you don't close it, your request will be missing the terminating boundary.
	if err := w.Close(); err != nil {
		return nil, err
	}

	return c.Do(ctx, http.MethodPost, reqURL, buf, options...)
}

// NewHTTPClient returns a new http client
func NewHTTPClient(client *http.Client) HTTPClient {
	return &httpclient{
		client: client,
	}
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
})

// HTTPGet issues a GET to the specified URL.
func HTTPGet(ctx context.Context, reqURL string, options ...HTTPOption) (*http.Response, error) {
	return defaultHTTPClient.Do(ctx, http.MethodGet, reqURL, nil, options...)
}

// HTTPPost issues a POST to the specified URL.
func HTTPPost(ctx context.Context, reqURL string, body []byte, options ...HTTPOption) (*http.Response, error) {
	return defaultHTTPClient.Do(ctx, http.MethodPost, reqURL, bytes.NewReader(body), options...)
}

// HTTPPostForm issues a POST to the specified URL, with data's keys and values URL-encoded as the request body.
func HTTPPostForm(ctx context.Context, reqURL string, data url.Values, options ...HTTPOption) (*http.Response, error) {
	options = append(options, WithHTTPHeader("Content-Type", "application/x-www-form-urlencoded"))

	return defaultHTTPClient.Do(ctx, http.MethodPost, reqURL, strings.NewReader(data.Encode()), options...)
}

// HTTPUpload issues a UPLOAD to the specified URL.
func HTTPUpload(ctx context.Context, reqURL string, form UploadForm, options ...HTTPOption) (*http.Response, error) {
	return defaultHTTPClient.Upload(ctx, reqURL, form, options...)
}

// HTTPDo sends an HTTP request and returns an HTTP response
func HTTPDo(ctx context.Context, method, reqURL string, body io.Reader, options ...HTTPOption) (*http.Response, error) {
	return defaultHTTPClient.Do(ctx, method, reqURL, body, options...)
}
