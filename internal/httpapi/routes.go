package httpapi

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/sse"

	"github.com/magne4000/easy-pz-docker/internal/backup"
	"github.com/magne4000/easy-pz-docker/internal/events"
	"github.com/magne4000/easy-pz-docker/internal/mods"
	"github.com/magne4000/easy-pz-docker/internal/publicapi"
	"github.com/magne4000/easy-pz-docker/internal/pz"
	"github.com/magne4000/easy-pz-docker/internal/pz/rcon"
	"github.com/magne4000/easy-pz-docker/internal/sched"
	"github.com/magne4000/easy-pz-docker/internal/settings"
	"github.com/magne4000/easy-pz-docker/internal/steam"
	"github.com/magne4000/easy-pz-docker/internal/store"
	"github.com/magne4000/easy-pz-docker/internal/sys"
	"github.com/magne4000/easy-pz-docker/internal/tasks"
)

const heartbeat = 15 * time.Second

// RegisterRoutes takes no listener and no config, so `pzman openapi` can call
// it with a zero Deps to emit the document.
func RegisterRoutes(api huma.API, d Deps) {
	registerServer(api, d)
	registerConsole(api, d)
	registerEvents(api, d)
	registerSystem(api, d)
	registerMods(api, d)
	registerBackups(api, d)
	registerSchedules(api, d)
	registerUpdates(api, d)
	registerConfig(api, d)
	// The public mod page's data.json is served outside /api. Referencing its
	// payload from an extension keeps the schema in the document (Huma prunes
	// unreferenced ones), which gives the frontend a generated type for it.
	o := api.OpenAPI()
	if o.Extensions == nil {
		o.Extensions = map[string]any{}
	}
	o.Extensions["x-public-mod-page"] = o.Components.Schemas.Schema(reflect.TypeFor[publicapi.PublicData](), true, "")
}

// mapErr translates domain errors to problem responses. Domain packages never import Huma.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	var busy *sched.BusyError
	var se huma.StatusError
	switch {
	case errors.As(err, &se):
		return err
	case errors.Is(err, store.ErrNotFound):
		return huma.Error404NotFound("not found")
	case errors.As(err, &busy), errors.Is(err, backup.ErrBusy), errors.Is(err, sched.ErrWindowOpen),
		errors.Is(err, pz.ErrNotRunning), errors.Is(err, pz.ErrAlreadyRunning):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, mods.ErrInvalidID), errors.Is(err, sched.ErrInvalidSchedule), errors.Is(err, steam.ErrCollectionNotFound):
		return huma.Error422UnprocessableEntity(err.Error())
	case errors.Is(err, backup.ErrMissing):
		return huma.Error410Gone(err.Error())
	}
	return huma.Error500InternalServerError(err.Error())
}

type Accepted struct {
	Status int
	Body   struct {
		Accepted bool   `json:"accepted"`
		Message  string `json:"message,omitempty"`
	}
}

func accepted(msg string) *Accepted {
	a := &Accepted{Status: http.StatusAccepted}
	a.Body.Accepted, a.Body.Message = true, msg
	return a
}

func op(id, method, path, tag, summary string, errs ...int) huma.Operation {
	return huma.Operation{OperationID: id, Method: method, Path: path, Tags: []string{tag}, Summary: summary, Errors: errs}
}

// ---------- server ----------

type ServerStatusOutput struct{ Body sched.ServerView }

type LifecycleInput struct {
	Body struct {
		CountdownMinutes int    `json:"countdownMinutes,omitempty" minimum:"0" maximum:"240" doc:"broadcast warnings, then act"`
		Message          string `json:"message,omitempty" maxLength:"200" doc:"countdown prefix, default 'Server restart'"`
	}
}

type MessageInput struct {
	Body struct {
		Message string `json:"message" minLength:"1" maxLength:"500"`
	}
}

type CancelOutput struct {
	Body struct {
		Cancelled bool `json:"cancelled"`
	}
}

func registerServer(api huma.API, d Deps) {
	huma.Register(api, op("get-server-status", http.MethodGet, "/server/status", "server", "Server state, players and current operation"),
		func(ctx context.Context, _ *struct{}) (*ServerStatusOutput, error) {
			return &ServerStatusOutput{Body: d.Coord.View(ctx)}, nil
		})
	huma.Register(api, op("start-server", http.MethodPost, "/server/start", "server", "Start the server", 409),
		func(ctx context.Context, _ *struct{}) (*Accepted, error) {
			if err := d.Coord.Start(ctx); err != nil {
				return nil, mapErr(err)
			}
			return accepted("starting"), nil
		})
	lifecycle := func(stop bool) func(context.Context, *LifecycleInput) (*Accepted, error) {
		return func(ctx context.Context, in *LifecycleInput) (*Accepted, error) {
			req := sched.LifecycleRequest{Countdown: time.Duration(in.Body.CountdownMinutes) * time.Minute, Message: in.Body.Message, Reason: "admin"}
			var err error
			if stop {
				err = d.Coord.Stop(req)
			} else {
				err = d.Coord.Restart(req)
			}
			if err != nil {
				return nil, mapErr(err)
			}
			return accepted(""), nil
		}
	}
	huma.Register(api, op("stop-server", http.MethodPost, "/server/stop", "server", "Save and stop, after an optional countdown", 409), lifecycle(true))
	huma.Register(api, op("restart-server", http.MethodPost, "/server/restart", "server", "Save and restart, after an optional countdown", 409), lifecycle(false))
	huma.Register(api, op("cancel-operation", http.MethodPost, "/server/cancel", "server", "Cancel a pending countdown"),
		func(ctx context.Context, _ *struct{}) (*CancelOutput, error) {
			out := &CancelOutput{}
			out.Body.Cancelled = d.Coord.CancelOperation()
			return out, nil
		})
	noContent := func(o huma.Operation) huma.Operation { o.DefaultStatus = http.StatusNoContent; return o }
	huma.Register(api, noContent(op("save-world", http.MethodPost, "/server/save", "server", "Save the world (RCON save)", 409)),
		func(ctx context.Context, _ *struct{}) (*struct{}, error) {
			return nil, mapErr(d.Coord.Save(ctx))
		})
	huma.Register(api, noContent(op("broadcast", http.MethodPost, "/server/broadcast", "server", "Broadcast a message to players", 409)),
		func(ctx context.Context, in *MessageInput) (*struct{}, error) {
			return nil, mapErr(d.Coord.Broadcast(ctx, in.Body.Message))
		})
}

// ---------- console ----------

type ConsoleHistoryInput struct {
	Limit int    `query:"limit" default:"500" minimum:"1" maximum:"5000" doc:"max lines"`
	Since uint64 `query:"since" doc:"return lines after this seq"`
}

type ConsoleHistoryOutput struct {
	Body struct {
		Lines []events.ConsoleLine `json:"lines"`
		Gap   bool                 `json:"gap"`
	}
}

type RconInput struct {
	Body struct {
		Command string `json:"command" minLength:"1" maxLength:"1000"`
	}
}

type RconOutput struct {
	Body struct {
		Command  string `json:"command"`
		Response string `json:"response"`
		Rejected bool   `json:"rejected" doc:"PZ answered with a known rejection string"`
		Message  string `json:"message,omitempty"`
	}
}

func registerConsole(api huma.API, d Deps) {
	huma.Register(api, op("get-console-history", http.MethodGet, "/console/history", "console", "Console backlog from the ring buffer"),
		func(ctx context.Context, in *ConsoleHistoryInput) (*ConsoleHistoryOutput, error) {
			out := &ConsoleHistoryOutput{}
			if in.Since > 0 {
				out.Body.Lines, out.Body.Gap = d.Ring.Since(in.Since)
				if len(out.Body.Lines) > in.Limit {
					out.Body.Lines = out.Body.Lines[len(out.Body.Lines)-in.Limit:]
					out.Body.Gap = true
				}
			} else {
				out.Body.Lines = d.Ring.Tail(in.Limit)
			}
			return out, nil
		})
	huma.Register(api, op("rcon-exec", http.MethodPost, "/console/rcon", "console", "Run an RCON command", 409, 502),
		func(ctx context.Context, in *RconInput) (*RconOutput, error) {
			cmd := strings.TrimSpace(in.Body.Command)
			d.Ring.Append("> " + cmd)
			resp, err := d.Coord.Exec(ctx, cmd)
			out := &RconOutput{}
			out.Body.Command, out.Body.Response = cmd, resp
			var rej *rcon.RejectedError
			switch {
			case errors.As(err, &rej):
				out.Body.Rejected, out.Body.Message = true, rej.Message
			case errors.Is(err, pz.ErrNotRunning):
				return nil, mapErr(err)
			case err != nil:
				return nil, huma.Error502BadGateway("RCON: " + err.Error())
			}
			for _, l := range strings.Split(strings.TrimRight(resp, "\n"), "\n") {
				if l != "" {
					d.Bus.Publish(d.Ring.Append(l))
				}
			}
			return out, nil
		})
}

// ---------- events (SSE) ----------

func registerEvents(api huma.API, d Deps) {
	sse.Register(api, huma.Operation{
		OperationID: "stream-events", Method: http.MethodGet, Path: "/events", Tags: []string{"events"},
		Summary: "Live event stream (invalidation signals)",
	}, events.EventTypeMap(), func(ctx context.Context, _ *struct{}, send sse.Sender) {
		ch, cancel := d.Bus.Subscribe()
		defer cancel()
		ticker := time.NewTicker(heartbeat)
		defer ticker.Stop()
		if err := send.Comment("connected"); err != nil {
			return
		}
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}
				if err := send.Data(ev); err != nil {
					return
				}
			case <-ticker.C:
				if err := send.Comment("hb"); err != nil {
					return
				}
			}
		}
	})
}

// ---------- system, settings, tasks ----------

type SystemOutput struct {
	Body struct {
		Version      string       `json:"version"`
		ServerName   string       `json:"serverName"`
		NonSteam     bool         `json:"nonSteam"`
		Drivers      string       `json:"drivers"`
		Scenario     string       `json:"scenario,omitempty"`
		Timezone     string       `json:"timezone"`
		Time         time.Time    `json:"time"`
		Disks        []sys.Volume `json:"disks"`
		DiskLevel    string       `json:"diskLevel"`
		PUID         int          `json:"puid"`
		PGID         int          `json:"pgid"`
		ModsPagePath string       `json:"modsPagePath" doc:"unlisted public mod page path (empty when disabled)"`
		Branch       string       `json:"branch"`
		Subscribers  int          `json:"subscribers"`
	}
}

type SettingsOutput struct{ Body settings.Settings }
type SettingsInput struct{ Body settings.Settings }

type TasksOutput struct {
	Body struct {
		Items []tasks.Task `json:"items"`
	}
}

func modsPagePath(d Deps) string {
	if !d.Settings.Get().PublicModsPage {
		return ""
	}
	return "/mods/" + d.Settings.ModsToken() + "/"
}

func registerSystem(api huma.API, d Deps) {
	huma.Register(api, op("get-system", http.MethodGet, "/system", "system", "Panel info and disk usage"),
		func(ctx context.Context, _ *struct{}) (*SystemOutput, error) {
			out := &SystemOutput{}
			b := &out.Body
			b.Version, b.ServerName, b.NonSteam, b.Drivers = d.Version, d.Cfg.ServerName, !d.Cfg.UseSteam, string(d.Cfg.Drivers)
			if d.Cfg.Drivers == "fake" {
				b.Scenario = d.Cfg.Scenario
			}
			b.Timezone, b.Time, b.PUID, b.PGID, b.Branch = d.Cfg.Timezone, time.Now().UTC(), d.Cfg.PUID, d.Cfg.PGID, d.Cfg.ServerBranch
			b.Disks = d.Disk.Snapshot()
			b.DiskLevel = string(sys.Worst(b.Disks))
			b.ModsPagePath = modsPagePath(d)
			b.Subscribers = d.Bus.Stats().Subscribers
			return out, nil
		})
	huma.Register(api, op("get-settings", http.MethodGet, "/settings", "system", "Panel settings"),
		func(ctx context.Context, _ *struct{}) (*SettingsOutput, error) {
			return &SettingsOutput{Body: d.Settings.Get()}, nil
		})
	huma.Register(api, op("update-settings", http.MethodPut, "/settings", "system", "Update panel settings"),
		func(ctx context.Context, in *SettingsInput) (*SettingsOutput, error) {
			s, err := d.Settings.Update(ctx, in.Body)
			if err != nil {
				return nil, huma.Error422UnprocessableEntity(err.Error())
			}
			return &SettingsOutput{Body: s}, nil
		})
	huma.Register(api, op("list-tasks", http.MethodGet, "/tasks", "system", "Recent long-running operations"),
		func(ctx context.Context, _ *struct{}) (*TasksOutput, error) {
			out := &TasksOutput{}
			out.Body.Items = d.Tasks.List()
			return out, nil
		})
}

func op204(id, method, path, tag, summary string, errs ...int) huma.Operation {
	o := op(id, method, path, tag, summary, errs...)
	o.DefaultStatus = http.StatusNoContent
	return o
}
