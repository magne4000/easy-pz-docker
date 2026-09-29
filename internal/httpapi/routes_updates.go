package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/magne4000/easy-pz-docker/internal/sched"
)

type UpdatesOutput struct {
	Body struct {
		sched.UpdateStatus
		NextCheckAt time.Time `json:"nextCheckAt,omitzero"`
	}
}

type ApplyUpdateInput struct {
	Body struct {
		Force bool `json:"force,omitempty" doc:"do not wait for the server to be empty"`
	}
}

func updatesOut(d Deps, st sched.UpdateStatus) *UpdatesOutput {
	out := &UpdatesOutput{}
	out.Body.UpdateStatus = st
	_, out.Body.NextCheckAt = d.Sched.NextInternal()
	return out
}

func registerUpdates(api huma.API, d Deps) {
	huma.Register(api, op("get-updates", http.MethodGet, "/updates", "updates", "Installed/latest build, mod updates and the update window"),
		func(ctx context.Context, _ *struct{}) (*UpdatesOutput, error) {
			return updatesOut(d, d.Coord.UpdateStatus()), nil
		})
	huma.Register(api, op("check-updates", http.MethodPost, "/updates/check", "updates", "Check for game and mod updates now"),
		func(ctx context.Context, _ *struct{}) (*UpdatesOutput, error) {
			st, _ := d.Coord.CheckUpdates(ctx)
			return updatesOut(d, st), nil
		})
	huma.Register(api, op("apply-update", http.MethodPost, "/updates/apply", "updates",
		"Open the update window: wait for empty (or force), save, stop, update game + mods, start", 409),
		func(ctx context.Context, in *ApplyUpdateInput) (*Accepted, error) {
			if err := d.Coord.OpenWindow("manual", in.Body.Force); err != nil {
				return nil, mapErr(err)
			}
			return accepted("update window opened"), nil
		})
	huma.Register(api, op204("cancel-update", http.MethodPost, "/updates/cancel", "updates", "Cancel an update window that is still waiting", 409),
		func(ctx context.Context, _ *struct{}) (*struct{}, error) {
			if err := d.Coord.CancelWindow(); err != nil {
				return nil, huma.Error409Conflict(err.Error())
			}
			return nil, nil
		})
}
