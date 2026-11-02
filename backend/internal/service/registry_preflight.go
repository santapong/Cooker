package service

import (
	"context"

	"github.com/santapong/cooker/internal/model"
)

// CheckAppRegistryPreflight checks only authored selections and current registry
// metadata. It creates no accepted authority, pins, or provider/credential reads.
// Admission and workers must still prepare/validate immutable registry evidence.
func CheckAppRegistryPreflight(ctx context.Context, authority *RegistryAuthorityService, app *model.App) error {
	ids, err := AppRegistrySelection(app)
	if err != nil {
		return err
	}
	if err := validateAppRegistryDeployment(app); err != nil {
		return err
	}
	return checkRegistryMetadata(ctx, authority, model.RegistrySubject{Kind: "app", ID: app.ID}, ids, nil)
}

// CheckPipelineRegistryPreflight rejects unsupported transports and ungranted
// selections before any source operation. It does not inspect runtime values.
func CheckPipelineRegistryPreflight(ctx context.Context, authority *RegistryAuthorityService, p *model.Pipeline) error {
	ids, bindings, err := PipelineRegistrySelection(p)
	if err != nil {
		return err
	}
	if err := validateNativeRegistryTransports(p); err != nil {
		return err
	}
	return checkRegistryMetadata(ctx, authority, model.RegistrySubject{Kind: "pipeline", ID: p.ID}, ids, bindings)
}

func checkRegistryMetadata(ctx context.Context, authority *RegistryAuthorityService, subject model.RegistrySubject, ids []string, bindings map[string][]string) error {
	plan := &model.RegistryAuthorityPlan{Version: 1, Subject: subject, Connections: map[string]model.RegistryConnectionPin{}, Stages: bindings}
	for _, id := range ids {
		if authority == nil || authority.Registries == nil {
			return ErrRegistryAuthorityRequired
		}
		r, err := authority.Registries.Get(ctx, id)
		if err != nil || r == nil || r.ID != id || r.Version < 1 || !granted(r, subject) {
			return ErrRegistryAuthorityDenied
		}
		pin, err := registryPin(r)
		if err != nil {
			return model.ErrInvalidRegistryAuthority
		}
		plan.Connections[id] = pin
	}
	if err := checkRegistryHosts(plan); err != nil {
		return err
	}
	return plan.Validate()
}

func (a *Automation) checkRegistryPreflight(ctx context.Context, app *model.App) error {
	ids, err := AppRegistrySelection(app)
	if err != nil {
		return err
	}
	if a.RegistryPreflight == nil {
		if a.RequireRegistryPreflight || len(ids) != 0 {
			return ErrRegistryAuthorityRequired
		}
		return nil // Explicitly unconfigured fixtures cannot select credentials.
	}
	if a.RegistryPreflight(ctx, app) != nil {
		return ErrRegistryAuthorityDenied // Never persist arbitrary callback errors.
	}
	return nil
}

func (a *PipelineAutomation) checkRegistryPreflight(ctx context.Context, p *model.Pipeline) error {
	ids, _, err := PipelineRegistrySelection(p)
	if err != nil {
		return err
	}
	if a.RegistryPreflight == nil {
		if a.RequireRegistryPreflight || len(ids) != 0 {
			return ErrRegistryAuthorityRequired
		}
		return nil
	}
	if a.RegistryPreflight(ctx, p) != nil {
		return ErrRegistryAuthorityDenied
	}
	return nil
}
