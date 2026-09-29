package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/magne4000/easy-pz-docker/internal/sched"
)

type SchedulesOutput struct {
	Body struct {
		Items             []sched.Schedule `json:"items"`
		Timezone          string           `json:"timezone"`
		NextBackupAt      time.Time        `json:"nextBackupAt,omitzero"`
		NextUpdateCheckAt time.Time        `json:"nextUpdateCheckAt,omitzero"`
	}
}

type ScheduleBody struct{ Body sched.ScheduleInput }

type ScheduleUpdate struct {
	ID   int64 `path:"id"`
	Body sched.ScheduleInput
}

type ScheduleOutput struct{ Body sched.Schedule }

type ScheduleIDInput struct {
	ID int64 `path:"id"`
}

func registerSchedules(api huma.API, d Deps) {
	huma.Register(api, op("list-schedules", http.MethodGet, "/schedules", "schedules", "Scheduled restarts, saves, broadcasts and backups"),
		func(ctx context.Context, _ *struct{}) (*SchedulesOutput, error) {
			list, err := d.Sched.List(ctx)
			if err != nil {
				return nil, mapErr(err)
			}
			out := &SchedulesOutput{}
			out.Body.Items, out.Body.Timezone = list, d.Cfg.Timezone
			out.Body.NextBackupAt, out.Body.NextUpdateCheckAt = d.Sched.NextInternal()
			return out, nil
		})
	huma.Register(api, op("create-schedule", http.MethodPost, "/schedules", "schedules", "Create a schedule", 422),
		func(ctx context.Context, in *ScheduleBody) (*ScheduleOutput, error) {
			s, err := d.Sched.Create(ctx, in.Body)
			if err != nil {
				return nil, mapErr(err)
			}
			return &ScheduleOutput{Body: s}, nil
		})
	huma.Register(api, op("update-schedule", http.MethodPut, "/schedules/{id}", "schedules", "Update a schedule", 404, 422),
		func(ctx context.Context, in *ScheduleUpdate) (*ScheduleOutput, error) {
			s, err := d.Sched.Update(ctx, in.ID, in.Body)
			if err != nil {
				return nil, mapErr(err)
			}
			return &ScheduleOutput{Body: s}, nil
		})
	huma.Register(api, op204("delete-schedule", http.MethodDelete, "/schedules/{id}", "schedules", "Delete a schedule", 404),
		func(ctx context.Context, in *ScheduleIDInput) (*struct{}, error) {
			return nil, mapErr(d.Sched.Delete(ctx, in.ID))
		})
	huma.Register(api, op("run-schedule", http.MethodPost, "/schedules/{id}/run", "schedules", "Run a schedule now", 404),
		func(ctx context.Context, in *ScheduleIDInput) (*Accepted, error) {
			if err := d.Sched.RunNow(ctx, in.ID); err != nil {
				return nil, mapErr(err)
			}
			return accepted(""), nil
		})
}
