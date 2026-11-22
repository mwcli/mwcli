package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// UploadOptions are the choices of Upload, named as in the API.
type UploadOptions struct {
	// Comment is the upload summary, shown in the file history and the log.
	Comment string
	// Text is the wikitext of the description page of a new file.
	Text string
	// IgnoreWarnings uploads even when the wiki warns, such as about a file
	// that exists or has the same content as another.
	IgnoreWarnings bool
	// ChunkSize is the most bytes one request sends; a larger file goes in
	// parts through the upload stash. 0 means DefaultChunkSize.
	ChunkSize int64
	// Progress, when not nil, is told how many bytes have been sent.
	Progress func(sent, total int64)
}

// DefaultChunkSize is the size of the parts of a large upload.
const DefaultChunkSize = 5 << 20

// UploadResult is the outcome of an upload.
type UploadResult struct {
	// FileName is the name the wiki saved the file under, without File:.
	FileName string
	// URL is the address of the uploaded file.
	URL string
}

// UploadWarnings are the warnings that stopped an upload, by code, such as
// exists, duplicate or was-deleted, with what the wiki said about each.
type UploadWarnings map[string]string

func (w UploadWarnings) Error() string {
	codes := make([]string, 0, len(w))
	for k := range w {
		codes = append(codes, k)
	}
	sort.Strings(codes)
	parts := make([]string, 0, len(codes))
	for _, k := range codes {
		if w[k] == "" {
			parts = append(parts, k)
		} else {
			parts = append(parts, k+": "+w[k])
		}
	}
	return "the wiki warned about the upload: " + strings.Join(parts, "; ")
}

// Upload uploads size bytes from r as filename. A file larger than the chunk
// size is sent in parts. When the wiki warns and IgnoreWarnings is not set,
// nothing is published and the error is UploadWarnings.
func (c *Client) Upload(ctx context.Context, filename string, r io.Reader, size int64, opts UploadOptions) (*UploadResult, error) {
	if err := c.ensureLogin(ctx); err != nil {
		return nil, err
	}
	token, err := c.TokenOf(ctx, "csrf")
	if err != nil {
		return nil, err
	}
	chunk := opts.ChunkSize
	if chunk <= 0 {
		chunk = DefaultChunkSize
	}
	final := url.Values{"action": {"upload"}, "assert": {"user"}, "filename": {filename}, "comment": {opts.Comment}, "token": {token}}
	if opts.Text != "" {
		final.Set("text", opts.Text)
	}
	if opts.IgnoreWarnings {
		final.Set("ignorewarnings", "1")
	}
	if size <= chunk {
		body, err := c.multipart(ctx, final, "file", filename, r, size)
		if err != nil {
			return nil, err
		}
		if opts.Progress != nil {
			opts.Progress(size, size)
		}
		return c.uploadResult(body)
	}
	// In parts: each goes to the stash, then the stashed file is published.
	var filekey string
	for offset := int64(0); offset < size; offset += chunk {
		n := min(chunk, size-offset)
		params := url.Values{
			"action": {"upload"}, "assert": {"user"}, "stash": {"1"}, "filename": {filename},
			"filesize": {strconv.FormatInt(size, 10)}, "offset": {strconv.FormatInt(offset, 10)},
			"ignorewarnings": {"1"}, "token": {token},
		}
		if filekey != "" {
			params.Set("filekey", filekey)
		}
		body, err := c.multipart(ctx, params, "chunk", filename, io.LimitReader(r, n), n)
		if err == nil {
			err = CheckError(body)
		}
		if err != nil {
			return nil, fmt.Errorf("upload part at byte %d: %w", offset, err)
		}
		var res struct {
			Upload struct {
				Result  string `json:"result"`
				FileKey string `json:"filekey"`
			} `json:"upload"`
		}
		if err := json.Unmarshal(body, &res); err != nil {
			return nil, fmt.Errorf("decode API response: %w", err)
		}
		switch res.Upload.Result {
		case "Continue", "Success":
		default:
			return nil, fmt.Errorf("upload part at byte %d: result %q", offset, res.Upload.Result)
		}
		if res.Upload.FileKey != "" {
			filekey = res.Upload.FileKey
		}
		if opts.Progress != nil {
			opts.Progress(offset+n, size)
		}
	}
	if filekey == "" {
		return nil, errors.New("the wiki returned no key for the stashed file")
	}
	final.Set("filekey", filekey)
	body, err := c.Raw(ctx, http.MethodPost, withJSON(final))
	if err != nil {
		return nil, err
	}
	return c.uploadResult(body)
}

// withJSON adds format=json and formatversion=2 to params.
func withJSON(params url.Values) url.Values {
	p := url.Values{}
	for k, v := range params {
		p[k] = v
	}
	p.Set("format", "json")
	p.Set("formatversion", "2")
	return p
}

// uploadResult reads the response of a published upload.
func (c *Client) uploadResult(body []byte) (*UploadResult, error) {
	if err := CheckError(body); err != nil {
		var ae *APIError
		if errors.As(err, &ae) && ae.Code == "fileexists-no-change" {
			return nil, fmt.Errorf("the file is the same as the latest version: %w", err)
		}
		return nil, err
	}
	var res struct {
		Upload struct {
			Result    string                     `json:"result"`
			Filename  string                     `json:"filename"`
			Warnings  map[string]json.RawMessage `json:"warnings"`
			ImageInfo struct {
				URL string `json:"url"`
			} `json:"imageinfo"`
		} `json:"upload"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("decode API response: %w", err)
	}
	switch res.Upload.Result {
	case "Success":
		return &UploadResult{FileName: res.Upload.Filename, URL: c.normalizeFileURL(res.Upload.ImageInfo.URL)}, nil
	case "Warning":
		w := UploadWarnings{}
		for k, raw := range res.Upload.Warnings {
			w[k] = warningText(raw)
		}
		return nil, w
	}
	return nil, fmt.Errorf("upload did not succeed (result: %q)", res.Upload.Result)
}

// warningText turns the value of an upload warning, a name, a list of names
// or an object, into one line.
func warningText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return strings.Join(list, ", ")
	}
	var b bytes.Buffer
	if json.Compact(&b, raw) == nil && b.String() != "true" && b.String() != "{}" {
		return b.String()
	}
	return ""
}

// ensureLogin logs in with a bot password before the first request.
func (c *Client) ensureLogin(ctx context.Context) error {
	if c.botName != "" && c.botState == 0 {
		c.botState = 1
		c.botErr = c.BotLogin(ctx, c.botName, c.botPassword, c.loginDomain)
		c.botState = 2
	}
	return c.botErr
}

// multipart POSTs params with n bytes of r as the file field and returns the
// response body. An API error is returned in the body, not as an error. The
// body is streamed with its Content-Length, as some servers in front of PHP
// do not pass on a chunked request body. It sends no maxlag: a request
// refused for lag could not be sent again, as r is read as it goes; the final
// publish of a stashed file goes through Raw and has maxlag.
func (c *Client) multipart(ctx context.Context, params url.Values, field, filename string, r io.Reader, n int64) ([]byte, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fields := withJSON(params)
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, v := range fields[k] {
			if err := mw.WriteField(k, v); err != nil {
				return nil, err
			}
		}
	}
	if _, err := mw.CreateFormFile(field, filename); err != nil {
		return nil, err
	}
	headLen := buf.Len()
	if err := mw.Close(); err != nil {
		return nil, err
	}
	all := buf.Bytes()
	head, tail := all[:headLen], all[headLen:]
	body := io.MultiReader(bytes.NewReader(head), &exactReader{r: r, n: n}, bytes.NewReader(tail))
	u := *c.url
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), body)
	if err != nil {
		return nil, err
	}
	req.ContentLength = int64(len(head)) + n + int64(len(tail))
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("User-Agent", c.UserAgent)
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if c.oauth1 != nil {
		// A multipart body is not part of the OAuth 1.0a signature.
		c.oauth1.sign(req, nil)
	}
	hc := *c.http
	hc.Timeout = 0
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, &HTTPError{Status: resp.Status, Body: strings.TrimSpace(string(b))}
	}
	return io.ReadAll(resp.Body)
}

// exactReader reads n bytes of r and fails if r ends sooner, such as a file
// that shrank after its size was taken.
type exactReader struct {
	r io.Reader
	n int64
}

func (e *exactReader) Read(p []byte) (int, error) {
	if e.n <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > e.n {
		p = p[:e.n]
	}
	k, err := e.r.Read(p)
	e.n -= int64(k)
	if err == io.EOF && e.n > 0 {
		return k, io.ErrUnexpectedEOF
	}
	if e.n == 0 && err == io.EOF {
		err = nil
	}
	return k, err
}
