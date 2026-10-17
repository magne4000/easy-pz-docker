package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gofiber/fiber/v3"

	"github.com/magne4000/easy-pz-docker/internal/mods"
)

const (
	uploadPath = "/api/mods/local"
	// uploadIdle bounds a stalled upload; a long but moving one goes on.
	uploadIdle = time.Minute
	// maxPathLen bounds one upload path field.
	maxPathLen = 4096
)

// limitBody keeps Fiber's body limit on every route but the mod upload.
// StreamRequestBody hands bodies over the limit, and chunked ones, to the
// router as streams instead of rejecting them, so they are rejected here.
// Whatever answers a streamed body before reading it all (this, auth, a
// refused upload) leaves the rest on the connection, where it would be parsed
// as the next request: the connection closes after the response.
func limitBody(limit int) fiber.Handler {
	return func(c fiber.Ctx) error {
		if n := c.Request().Header.ContentLength(); n > limit || n == -1 {
			c.RequestCtx().SetConnectionClose()
			if c.Method() != fiber.MethodPost || c.Path() != uploadPath {
				return fiber.ErrRequestEntityTooLarge
			}
		}
		return c.Next()
	}
}

// idleReader pushes the connection's read deadline forward before every
// read, so ReadTimeout does not cut a long upload that keeps moving.
type idleReader struct {
	r    io.Reader
	conn net.Conn
}

func (r idleReader) Read(p []byte) (int, error) {
	if r.conn != nil {
		_ = r.conn.SetReadDeadline(time.Now().Add(uploadIdle))
	}
	return r.r.Read(p)
}

// srcReader keeps the error of reading the request (the client's, e.g. a
// dropped upload) apart from WriteFile's disk errors.
type srcReader struct {
	r   io.Reader
	err error
}

func (s *srcReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		s.err = err
	}
	return n, err
}

type UploadLocalInput struct {
	Replace bool `query:"replace" doc:"replace the local mod with the same folder; refused while the server runs"`
}

type UploadLocalOutput struct {
	Body struct {
		Folder string `json:"folder" doc:"the installed folder in the mods folder"`
	}
}

func registerUpload(api huma.API, d Deps) {
	o := op("upload-local-mod", http.MethodPost, "/mods/local", "mods", "Upload a mod folder into the server's mods folder", 409, 413, 422)
	o.Description = "multipart/form-data, streamed to disk: for each file a `path` field (slash-separated, starting with the mod folder) then a `file` field with its content."
	o.RequestBody = &huma.RequestBody{Required: true, Content: map[string]*huma.MediaType{"multipart/form-data": {}}}
	huma.Register(api, o, func(ctx context.Context, in *UploadLocalInput) (*UploadLocalOutput, error) {
		c := fiberCtx(ctx)
		_, params, err := mime.ParseMediaType(string(c.Request().Header.ContentType()))
		if err != nil || params["boundary"] == "" {
			return nil, huma.Error400BadRequest("expected a multipart/form-data body")
		}
		up, err := d.Mods.BeginLocalUpload()
		if err != nil {
			return nil, mapErr(err)
		}
		defer up.Abort()
		mr := multipart.NewReader(idleReader{r: c.Request().BodyStream(), conn: c.RequestCtx().Conn()}, params["boundary"])
		path := ""
		for {
			p, err := mr.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, huma.Error400BadRequest("reading the upload: " + err.Error())
			}
			switch p.FormName() {
			case "path":
				b, err := io.ReadAll(io.LimitReader(p, maxPathLen+1))
				if err != nil {
					return nil, huma.Error400BadRequest("reading the upload: " + err.Error())
				}
				if len(b) > maxPathLen {
					return nil, mapErr(fmt.Errorf("%w: a path is longer than %d bytes", mods.ErrInvalidUpload, maxPathLen))
				}
				path = string(b)
			case "file":
				if path == "" {
					return nil, huma.Error400BadRequest("a file part must follow its path part")
				}
				src := &srcReader{r: p}
				if err := up.WriteFile(path, src); err != nil {
					if src.err != nil {
						return nil, huma.Error400BadRequest("reading the upload: " + src.err.Error())
					}
					return nil, mapErr(err)
				}
				path = ""
			default:
				return nil, huma.Error400BadRequest("unexpected part " + strings.TrimSpace(p.FormName()))
			}
		}
		folder, err := d.Mods.CommitLocalUpload(ctx, up, in.Replace)
		if err != nil {
			return nil, mapErr(err)
		}
		out := &UploadLocalOutput{}
		out.Body.Folder = folder
		return out, nil
	})
}
