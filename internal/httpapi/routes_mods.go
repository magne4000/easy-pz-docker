package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/magne4000/easy-pz-docker/internal/mods"
)

type ModsOutput struct{ Body mods.Overview }

type AddModsInput struct {
	Body struct {
		WorkshopIDs []string `json:"workshopIds" minItems:"1" maxItems:"500"`
	}
}

type AddedOutput struct {
	Body struct {
		Added []string `json:"added"`
	}
}

type WorkshopIDInput struct {
	WorkshopID string `path:"workshopId" pattern:"^[0-9]+$"`
}

type ToggleModInput struct {
	Body struct {
		WorkshopID string `json:"workshopId" pattern:"^[0-9]+$"`
		ModID      string `json:"modId" minLength:"1"`
		Enabled    bool   `json:"enabled"`
	}
}

type OrderInput struct {
	Body struct {
		ModIDs []string `json:"modIds"`
	}
}

type ModUpdatesOutput struct {
	Body struct {
		Updates []string `json:"updates" doc:"workshop ids with a newer or missing download"`
		Error   string   `json:"error,omitempty"`
	}
}

type CollectionInput struct {
	Body struct {
		CollectionID string `json:"collectionId" pattern:"^[0-9]+$"`
	}
}

type ConflictsOutput struct {
	Body struct {
		Conflicts []mods.Conflict `json:"conflicts"`
	}
}

type LinkResultOutput struct{ Body mods.LinkResult }

func registerMods(api huma.API, d Deps) {
	huma.Register(api, op("list-mods", http.MethodGet, "/mods", "mods", "Tracked workshop items, load order and maps"),
		func(ctx context.Context, _ *struct{}) (*ModsOutput, error) {
			ov, err := d.Mods.Overview(ctx)
			if err != nil {
				return nil, mapErr(err)
			}
			return &ModsOutput{Body: ov}, nil
		})
	huma.Register(api, op("add-mods", http.MethodPost, "/mods", "mods", "Track workshop items and download them", 422),
		func(ctx context.Context, in *AddModsInput) (*AddedOutput, error) {
			added, err := d.Mods.Add(ctx, in.Body.WorkshopIDs)
			if err != nil {
				return nil, mapErr(err)
			}
			out := &AddedOutput{}
			out.Body.Added = added
			return out, nil
		})
	huma.Register(api, op204("remove-mod", http.MethodDelete, "/mods/{workshopId}", "mods", "Untrack an item, unlink it and delete its download", 404),
		func(ctx context.Context, in *WorkshopIDInput) (*struct{}, error) {
			return nil, mapErr(d.Mods.Remove(ctx, in.WorkshopID))
		})
	huma.Register(api, op204("toggle-mod", http.MethodPost, "/mods/toggle", "mods", "Enable or disable one mod id", 404),
		func(ctx context.Context, in *ToggleModInput) (*struct{}, error) {
			return nil, mapErr(d.Mods.SetEnabled(ctx, in.Body.WorkshopID, in.Body.ModID, in.Body.Enabled))
		})
	huma.Register(api, op204("set-mod-order", http.MethodPut, "/mods/order", "mods", "Set the load order"),
		func(ctx context.Context, in *OrderInput) (*struct{}, error) {
			return nil, mapErr(d.Mods.SetOrder(ctx, in.Body.ModIDs))
		})
	huma.Register(api, op204("autosort-mods", http.MethodPost, "/mods/autosort", "mods", "Sort the load order by require= dependencies"),
		func(ctx context.Context, _ *struct{}) (*struct{}, error) {
			return nil, mapErr(d.Mods.AutoSort(ctx))
		})
	huma.Register(api, op("check-mod-updates", http.MethodPost, "/mods/check", "mods", "Refresh Workshop metadata and detect outdated downloads"),
		func(ctx context.Context, _ *struct{}) (*ModUpdatesOutput, error) {
			ups, err := d.Mods.CheckUpdates(ctx)
			out := &ModUpdatesOutput{}
			out.Body.Updates = ups
			if err != nil {
				if ups == nil {
					return nil, mapErr(err)
				}
				out.Body.Error = err.Error()
			}
			return out, nil
		})
	huma.Register(api, op("import-collection", http.MethodPost, "/mods/collection", "mods", "Track every item of a Workshop collection", 422),
		func(ctx context.Context, in *CollectionInput) (*AddedOutput, error) {
			added, err := d.Mods.ImportCollection(ctx, in.Body.CollectionID)
			if err != nil {
				return nil, mapErr(err)
			}
			out := &AddedOutput{}
			out.Body.Added = added
			return out, nil
		})
	huma.Register(api, op("get-mod-conflicts", http.MethodGet, "/mods/conflicts", "mods", "Files overridden by more than one enabled mod"),
		func(ctx context.Context, _ *struct{}) (*ConflictsOutput, error) {
			cs, err := d.Mods.Conflicts(ctx)
			if err != nil {
				return nil, mapErr(err)
			}
			out := &ConflictsOutput{}
			out.Body.Conflicts = cs
			return out, nil
		})
	huma.Register(api, op("apply-mods", http.MethodPost, "/mods/apply", "mods", "Rewrite Mods=/WorkshopItems=/Map= and reconcile mod links now"),
		func(ctx context.Context, _ *struct{}) (*LinkResultOutput, error) {
			res, err := d.Mods.Apply(ctx)
			if err != nil {
				return nil, mapErr(err)
			}
			return &LinkResultOutput{Body: res}, nil
		})
}
