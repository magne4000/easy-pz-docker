package steam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrCollectionNotFound: the id is not a (visible) Workshop collection — e.g. a
// plain item id, or a private/deleted collection.
var ErrCollectionNotFound = errors.New("steam web api: not a workshop collection")

type FileDetails struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	FileSize    int64     `json:"fileSize"`
	TimeUpdated time.Time `json:"timeUpdated"`
	PreviewURL  string    `json:"previewUrl"`
	Result      int       `json:"result"`
	Tags        []string  `json:"tags"`
}

type WebAPI interface {
	PublishedFileDetails(ctx context.Context, ids []string) ([]FileDetails, error)
	CollectionDetails(ctx context.Context, collectionID string) ([]string, error)
}

type HTTPWebAPI struct {
	client  *http.Client
	baseURL string
}

func NewWebAPI(client *http.Client, baseURL string) *HTTPWebAPI {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	if baseURL == "" {
		baseURL = "https://api.steampowered.com"
	}
	return &HTTPWebAPI{client: client, baseURL: strings.TrimRight(baseURL, "/")}
}

// flexInt accepts both JSON numbers and numeric strings (Steam uses both).
type flexInt int64

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	*f = flexInt(n)
	return err
}

func (a *HTTPWebAPI) post(ctx context.Context, method string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/ISteamRemoteStorage/"+method+"/v1/", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("steam web api %s: %w", method, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("steam web api %s: HTTP %d", method, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("steam web api %s: %w", method, err)
	}
	return nil
}

func (a *HTTPWebAPI) PublishedFileDetails(ctx context.Context, ids []string) ([]FileDetails, error) {
	out := []FileDetails{}
	for start := 0; start < len(ids); start += 100 {
		batch := ids[start:min(start+100, len(ids))]
		form := url.Values{"itemcount": {strconv.Itoa(len(batch))}}
		for i, id := range batch {
			form.Set(fmt.Sprintf("publishedfileids[%d]", i), id)
		}
		var body struct {
			Response struct {
				Details []struct {
					ID          string  `json:"publishedfileid"`
					Result      int     `json:"result"`
					Title       string  `json:"title"`
					FileSize    flexInt `json:"file_size"`
					TimeUpdated flexInt `json:"time_updated"`
					Preview     string  `json:"preview_url"`
					Tags        []struct {
						Tag string `json:"tag"`
					} `json:"tags"`
				} `json:"publishedfiledetails"`
			} `json:"response"`
		}
		if err := a.post(ctx, "GetPublishedFileDetails", form, &body); err != nil {
			return nil, err
		}
		for _, d := range body.Response.Details {
			fd := FileDetails{ID: d.ID, Result: d.Result, Title: d.Title, FileSize: int64(d.FileSize), PreviewURL: d.Preview, Tags: []string{}}
			if d.TimeUpdated > 0 {
				fd.TimeUpdated = time.Unix(int64(d.TimeUpdated), 0).UTC()
			}
			for _, t := range d.Tags {
				fd.Tags = append(fd.Tags, t.Tag)
			}
			out = append(out, fd)
		}
	}
	return out, nil
}

func (a *HTTPWebAPI) CollectionDetails(ctx context.Context, collectionID string) ([]string, error) {
	form := url.Values{"collectioncount": {"1"}, "publishedfileids[0]": {collectionID}}
	var body struct {
		Response struct {
			Details []struct {
				Result   int `json:"result"`
				Children []struct {
					ID        string  `json:"publishedfileid"`
					SortOrder flexInt `json:"sortorder"`
					FileType  flexInt `json:"filetype"`
				} `json:"children"`
			} `json:"collectiondetails"`
		} `json:"response"`
	}
	if err := a.post(ctx, "GetCollectionDetails", form, &body); err != nil {
		return nil, err
	}
	if len(body.Response.Details) == 0 || body.Response.Details[0].Result != 1 {
		return nil, fmt.Errorf("%w: %s", ErrCollectionNotFound, collectionID)
	}
	ch := body.Response.Details[0].Children
	sort.SliceStable(ch, func(i, j int) bool { return ch[i].SortOrder < ch[j].SortOrder })
	out := []string{}
	for _, c := range ch {
		if c.FileType == 0 {
			out = append(out, c.ID)
		}
	}
	return out, nil
}
