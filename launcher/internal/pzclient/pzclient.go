package pzclient

import (
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
}

func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 0}}
}

func (c *Client) Fetch(ctx context.Context, page string) (*publicapi.PublicData, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, page+"data.json", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
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

var ErrNotReady = errors.New("the server is still preparing this download")

func (c *Client) Download(ctx context.Context, page, packURL, want string, f io.Writer, progress func(int64)) error {
	base, err := url.Parse(page)
	if err != nil {
		return err
	}
	ref, err := url.Parse(packURL)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.ResolveReference(ref).String(), nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusServiceUnavailable {
		return ErrNotReady
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: server answered %s", resp.Status)
	}
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

func (c *Client) DownloadFile(ctx context.Context, dir, page, packURL, want string, progress func(int64)) (string, error) {
	f, err := os.CreateTemp(dir, ".easypz-download-*.zip")
	if err != nil {
		return "", err
	}
	if err := c.Download(ctx, page, packURL, want, f, progress); err != nil {
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
