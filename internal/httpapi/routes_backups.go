package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/magne4000/easy-pz-docker/internal/backup"
	"github.com/magne4000/easy-pz-docker/internal/store"
)

type BackupView struct {
	ID          int64     `json:"id"`
	File        string    `json:"file"`
	CreatedAt   time.Time `json:"createdAt"`
	Size        int64     `json:"size"`
	Reason      string    `json:"reason"`
	Note        string    `json:"note"`
	Pinned      bool      `json:"pinned"`
	BuildID     string    `json:"buildId"`
	FileCount   int       `json:"fileCount"`
	ModCount    int       `json:"modCount"`
	Fingerprint string    `json:"fingerprint"`
}

func backupView(b store.Backup) BackupView {
	return BackupView{ID: b.ID, File: b.File, CreatedAt: b.CreatedAt, Size: b.Size, Reason: b.Reason, Note: b.Note, Pinned: b.Pinned,
		BuildID: b.BuildID, FileCount: b.FileCount, ModCount: b.ModCount, Fingerprint: b.Fingerprint}
}

type BackupsOutput struct {
	Body struct {
		Items     []BackupView   `json:"items"`
		Status    backup.Summary `json:"status"`
		Policy    backup.Policy  `json:"policy"`
		NextRunAt time.Time      `json:"nextRunAt,omitzero"`
	}
}

type CreateBackupInput struct {
	Body struct {
		Note string `json:"note,omitempty" maxLength:"200"`
		// A pointer: Huma replaces zero values with the default, so a plain
		// bool defaulting to true could never carry an explicit false.
		Force *bool `json:"force,omitempty" default:"true" doc:"back up even when the world is unchanged"`
	}
}

func (in *CreateBackupInput) force() bool { return in.Body.Force == nil || *in.Body.Force }

type BackupIDInput struct {
	ID int64 `path:"id"`
}

type PinInput struct {
	ID   int64 `path:"id"`
	Body struct {
		Pinned bool `json:"pinned"`
	}
}

type BackupDetailOutput struct {
	Body struct {
		Backup   BackupView      `json:"backup"`
		Manifest backup.Manifest `json:"manifest"`
	}
}

func policy(d Deps) backup.Policy {
	s := d.Settings.Get()
	return backup.Policy{Keep: s.BackupKeep, KeepDaily: s.BackupKeepDaily, KeepWeekly: s.BackupKeepWeekly, MaxTotalBytes: int64(s.BackupMaxTotalGB * (1 << 30))}
}

func registerBackups(api huma.API, d Deps) {
	huma.Register(api, op("list-backups", http.MethodGet, "/backups", "backups", "Backups, newest first"),
		func(ctx context.Context, _ *struct{}) (*BackupsOutput, error) {
			list, err := d.Backups.List(ctx)
			if err != nil {
				return nil, mapErr(err)
			}
			st, err := d.Backups.Status(ctx)
			if err != nil {
				return nil, mapErr(err)
			}
			out := &BackupsOutput{}
			out.Body.Items = make([]BackupView, 0, len(list))
			for _, b := range list {
				out.Body.Items = append(out.Body.Items, backupView(b))
			}
			out.Body.Status, out.Body.Policy = st, policy(d)
			out.Body.NextRunAt, _ = d.Sched.NextInternal()
			return out, nil
		})
	huma.Register(api, op("create-backup", http.MethodPost, "/backups", "backups", "Start a manual backup", 409),
		func(ctx context.Context, in *CreateBackupInput) (*Accepted, error) {
			if st, _ := d.Backups.Status(ctx); st.Running {
				return nil, mapErr(backup.ErrBusy)
			}
			go func() {
				created, err := d.Coord.RunBackup(context.Background(), "manual", in.Body.Note, in.force())
				switch {
				case err != nil:
					d.Ring.Append("[pzman] manual backup failed: " + err.Error())
				case !created:
					d.Ring.Append("[pzman] manual backup skipped: the world has not changed since the last backup")
				}
			}()
			return accepted("backup started"), nil
		})
	huma.Register(api, op("get-backup", http.MethodGet, "/backups/{id}", "backups", "Backup with its manifest", 404, 410),
		func(ctx context.Context, in *BackupIDInput) (*BackupDetailOutput, error) {
			b, err := d.Backups.Get(ctx, in.ID)
			if err != nil {
				return nil, mapErr(err)
			}
			m, err := d.Backups.Manifest(ctx, in.ID)
			if errors.Is(err, fs.ErrNotExist) {
				return nil, mapErr(backup.ErrMissing)
			}
			if err != nil {
				return nil, mapErr(err)
			}
			out := &BackupDetailOutput{}
			out.Body.Backup, out.Body.Manifest = backupView(b), m
			return out, nil
		})
	huma.Register(api, op204("pin-backup", http.MethodPut, "/backups/{id}/pin", "backups", "Pin (exempt from pruning) or unpin", 404),
		func(ctx context.Context, in *PinInput) (*struct{}, error) {
			return nil, mapErr(d.Backups.SetPinned(ctx, in.ID, in.Body.Pinned))
		})
	huma.Register(api, op204("delete-backup", http.MethodDelete, "/backups/{id}", "backups", "Delete a backup", 404),
		func(ctx context.Context, in *BackupIDInput) (*struct{}, error) {
			return nil, mapErr(d.Backups.Delete(ctx, in.ID))
		})
	huma.Register(api, op("restore-backup", http.MethodPost, "/backups/{id}/restore", "backups",
		"Stop the server, take a safety backup, restore, and start again if it was running", 404, 409),
		func(ctx context.Context, in *BackupIDInput) (*Accepted, error) {
			if err := d.Coord.Restore(in.ID); err != nil {
				return nil, mapErr(err)
			}
			return accepted("restore started"), nil
		})
	huma.Register(api, op("download-backup", http.MethodGet, "/backups/{id}/download", "backups", "Download the archive (tar+zstd)", 404, 410),
		func(ctx context.Context, in *BackupIDInput) (*huma.StreamResponse, error) {
			b, err := d.Backups.Get(ctx, in.ID)
			if err != nil {
				return nil, mapErr(err)
			}
			f, err := os.Open(d.Backups.Path(b))
			if err != nil {
				return nil, mapErr(backup.ErrMissing)
			}
			st, _ := f.Stat()
			return &huma.StreamResponse{Body: func(hc huma.Context) {
				defer f.Close()
				hc.SetHeader("Content-Type", "application/zstd")
				hc.SetHeader("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(b.File)))
				if st != nil {
					hc.SetHeader("Content-Length", strconv.FormatInt(st.Size(), 10))
				}
				io.Copy(hc.BodyWriter(), f)
			}}, nil
		})
}
