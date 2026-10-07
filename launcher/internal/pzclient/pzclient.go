package pzclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/magne4000/easy-pz-docker/internal/publicapi"
)

var pagePath = regexp.MustCompile(`^(.*/mods/[^/]+)/?(?:data\.json|index\.html)?$`)

func PageURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("paste the server's mod page link (http:// or https://)")
	}
	m := pagePath.FindStringSubmatch(u.Path)
	if m == nil {
		return "", errors.New("this does not look like a mod page link: it should contain /mods/<token>/")
	}
	u.Path, u.RawQuery, u.Fragment = m[1]+"/", "", ""
	return u.String(), nil
}

type Client struct {
	HTTP *http.Client
	// BusyWait bounds how long one request waits out 429 answers.
	BusyWait time.Duration
}

func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 0}, BusyWait: 5 * time.Minute}
}

var (
	ErrNotReady = errors.New("the server is still preparing this download")
	ErrBusy     = errors.New("the server is busy with other downloads from your network, try again in a minute")
	ErrChanged  = errors.New("the server's mods changed during the sync, sync again")
)

// do sends req and waits out 429 answers (the server's per-address limits)
// for their Retry-After, while ctx and BusyWait allow.
func (c *Client) do(req *http.Request) (*http.Response, error) {
	start := time.Now()
	for {
		resp, err := c.HTTP.Do(req)
		if err != nil || resp.StatusCode != http.StatusTooManyRequests {
			return resp, err
		}
		resp.Body.Close()
		wait := retryAfter(resp.Header.Get("Retry-After"))
		deadline, ok := req.Context().Deadline()
		if time.Since(start)+wait > c.BusyWait || (ok && time.Until(deadline) < wait) {
			return nil, ErrBusy
		}
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(wait):
		}
		if req.GetBody != nil {
			if req.Body, err = req.GetBody(); err != nil {
				return nil, err
			}
		}
	}
}

// retryAfter reads delay-seconds (the server never sends a date), at least a
// second so a misbehaving server is not hammered.
func retryAfter(h string) time.Duration {
	s, err := strconv.Atoi(strings.TrimSpace(h))
	if err != nil {
		return 5 * time.Second
	}
	return min(max(time.Duration(s)*time.Second, time.Second), time.Minute)
}

func (c *Client) Fetch(ctx context.Context, page string) (*publicapi.PublicData, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, page+"data.json", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req)
	if errors.Is(err, ErrBusy) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("reach server: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, errors.New("mod page not found: the link is wrong or the page was disabled")
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("server answered %s", resp.Status)
	}
	var d publicapi.PublicData
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&d); err != nil {
		return nil, fmt.Errorf("read server data: %w", err)
	}
	return &d, nil
}

// request resolves ref (a path from data.json) against the page.
func request(ctx context.Context, method, page, ref string, body []byte) (*http.Request, error) {
	base, err := url.Parse(page)
	if err != nil {
		return nil, err
	}
	r, err := url.Parse(ref)
	if err != nil {
		return nil, err
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, base.ResolveReference(r).String(), rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// send maps the answers shared by every download endpoint.
func (c *Client) send(req *http.Request) (*http.Response, error) {
	resp, err := c.do(req)
	if errors.Is(err, ErrBusy) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return resp, nil
	case http.StatusServiceUnavailable:
		err = ErrNotReady
	case http.StatusConflict:
		err = ErrChanged
	case http.StatusNotFound:
		err = errors.New("download: this mod is no longer on the server")
	default:
		err = fmt.Errorf("download: server answered %s", resp.Status)
	}
	resp.Body.Close()
	return nil, err
}

func (c *Client) Download(ctx context.Context, page, packURL, want string, f io.Writer, progress func(int64)) error {
	req, err := request(ctx, http.MethodGet, page, packURL, nil)
	if err != nil {
		return err
	}
	return c.save(req, want, f, progress)
}

// save streams the answer to f; want, when set, is its SHA-256.
func (c *Client) save(req *http.Request, want string, f io.Writer, progress func(int64)) error {
	resp, err := c.send(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	h := sha256.New()
	w := io.MultiWriter(f, h)
	var n int64
	buf := make([]byte, 256<<10)
	for {
		k, rerr := resp.Body.Read(buf)
		if k > 0 {
			if _, err := w.Write(buf[:k]); err != nil {
				return err
			}
			n += int64(k)
			if progress != nil {
				progress(n)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return fmt.Errorf("download: %w", rerr)
		}
	}
	if got := hex.EncodeToString(h.Sum(nil)); want != "" && !strings.EqualFold(got, want) {
		return fmt.Errorf("download is corrupted (checksum mismatch)")
	}
	return nil
}

// toTemp writes a download to a temp file in dir, removed on failure.
func toTemp(dir string, write func(io.Writer) error) (string, error) {
	f, err := os.CreateTemp(dir, ".easypz-download-*.zip")
	if err != nil {
		return "", err
	}
	if err := write(f); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func (c *Client) DownloadFile(ctx context.Context, dir, page, packURL, want string, progress func(int64)) (string, error) {
	return toTemp(dir, func(f io.Writer) error { return c.Download(ctx, page, packURL, want, f, progress) })
}

// Files fetches an item's per-file listing (PublicItem.Files).
func (c *Client) Files(ctx context.Context, page, filesURL string) (*publicapi.PackFiles, error) {
	req, err := request(ctx, http.MethodGet, page, filesURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.send(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var l publicapi.PackFiles
	if err := json.NewDecoder(io.LimitReader(resp.Body, 256<<20)).Decode(&l); err != nil {
		return nil, fmt.Errorf("read the file listing: %w", err)
	}
	return &l, nil
}

// DownloadFiles saves a zip of the listed files sel selects to a temp file in dir.
func (c *Client) DownloadFiles(ctx context.Context, dir, page, filesURL string, sel publicapi.FilesRequest, progress func(int64)) (string, error) {
	body, err := json.Marshal(sel)
	if err != nil {
		return "", err
	}
	return toTemp(dir, func(f io.Writer) error {
		req, err := request(ctx, http.MethodPost, page, filesURL, body)
		if err != nil {
			return err
		}
		return c.save(req, "", f, progress)
	})
}
