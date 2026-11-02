package vercel

import (
	"context"
	"errors"
	"sync"
	"time"
)

const enrichmentConcurrency = 4
const detailsFailure = "Vercel details could not be refreshed; any previous observation is retained"
const aliasesFailure = "Production aliases could not be refreshed; any previous observation is retained"

// enrich adds optional observations after the complete core inventory succeeds.
// Installation inventory runs serially after detail workers and shares their
// cooldown, keeping total optional request concurrency at or below four.
func (s *Service) enrich(ctx context.Context, r *Record, out *Snapshot) time.Duration {
	cooldown := s.enrichDetails(ctx, r, out)
	if delay := s.enrichIntegrations(ctx, r, out, cooldown); delay > cooldown {
		cooldown = delay
	}
	return cooldown
}

// enrichDetails adds optional project and deployment observations.
// At most four requests run concurrently; cancellation/rate limit stops new calls.
// All writes remain protected by the original connection version + sync lease.
func (s *Service) enrichDetails(ctx context.Context, r *Record, out *Snapshot) time.Duration {
	api, ok := s.API.(DetailsAPI)
	if !ok {
		return 0
	}
	token, err := s.token(r)
	if err != nil {
		return 0
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	previousProjects := map[string]Project{}
	previousDeployments := map[string]Deployment{}
	for _, p := range r.Snapshot.Projects {
		previousProjects[p.ID] = p
	}
	for _, d := range r.Snapshot.Deployments {
		previousDeployments[d.ProjectID+"/"+d.ID] = d
	}
	var mu sync.Mutex
	var cooldown time.Duration
	failed := func(err error) string {
		var apiErr *APIError
		if errors.As(err, &apiErr) && (apiErr.StatusCode == 429 || apiErr.RetryAfter > 0) {
			delay := apiErr.RetryAfter
			if delay < SyncInterval {
				delay = SyncInterval
			}
			if delay > 24*time.Hour {
				delay = 24 * time.Hour
			}
			mu.Lock()
			if delay > cooldown {
				cooldown = delay
			}
			mu.Unlock()
			cancel()
			return "Vercel details are rate limited; previous observations are retained until retry"
		}
		return detailsFailure
	}
	getDeployment := func(project, id string, prior *Deployment) *Deployment {
		var observed *Deployment
		callErr := ctx.Err()
		if callErr == nil {
			observed, callErr = api.DeploymentDetails(ctx, token, r.TeamID, id)
		}
		if callErr == nil && (observed == nil || observed.ID != id || observed.ProjectID != project || observed.Details == nil || (observed.Details.ownerID != "" && observed.Details.ownerID != r.Identity.ID)) {
			callErr = ErrInvalid
		}
		if callErr == nil {
			return observed
		}
		message := failed(callErr)
		if prior != nil && prior.ID == id && prior.ProjectID == project {
			d := cloneDeployment(*prior)
			if d.Details == nil {
				d.Details = &DeploymentDetails{Aliases: []string{}, Services: []ServiceDefinition{}}
			}
			d.Details.Error = message
			return &d
		}
		return &Deployment{ID: id, ProjectID: project, State: "UNKNOWN", Details: &DeploymentDetails{Aliases: []string{}, Services: []ServiceDefinition{}, Error: message}}
	}
	getAliases := func(current, prior *Deployment) {
		aliasesAPI, supported := s.API.(AliasesAPI)
		if !supported || current == nil || current.Details == nil {
			return
		}
		var retained *DeploymentAliasDetails
		if prior != nil && prior.ID == current.ID && prior.ProjectID == current.ProjectID && prior.Details != nil {
			retained = cloneAliasDetails(prior.Details.AliasDetails)
			if retained == nil && prior.Details.AliasAssigned && !prior.Details.ObservedAt.IsZero() {
				retained = &DeploymentAliasDetails{DeploymentID: current.ID, Aliases: append([]string{}, prior.Details.Aliases...), ObservedAt: prior.Details.ObservedAt}
			}
		}
		var result *DeploymentAliasDetails
		callErr := ctx.Err()
		if callErr == nil && current.Details.Error != "" {
			callErr = ErrInvalid // Do not read aliases before validating current deployment metadata.
		}
		if callErr == nil {
			result, callErr = aliasesAPI.DeploymentAliases(ctx, token, r.TeamID, current.ID)
		}
		if callErr == nil && (result == nil || result.DeploymentID != current.ID || result.ObservedAt.IsZero()) {
			callErr = ErrInvalid
		}
		if callErr == nil {
			current.Details.AliasDetails = cloneAliasDetails(result)
			return
		}
		message := aliasesFailure
		if failed(callErr) != detailsFailure {
			message = "Production aliases are rate limited; previous observations are retained until retry"
		}
		if retained == nil || retained.DeploymentID != current.ID {
			retained = &DeploymentAliasDetails{DeploymentID: current.ID, Aliases: []string{}}
		}
		retained.Error = message
		current.Details.AliasDetails = retained
	}
	jobs := make(chan int)
	var workers sync.WaitGroup
	for range enrichmentConcurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				p := &out.Projects[index]
				prior := previousProjects[p.ID].Details
				var details *ProjectDetails
				callErr := ctx.Err()
				if callErr == nil {
					details, callErr = api.ProjectDetails(ctx, token, r.TeamID, p.ID)
				}
				if callErr == nil && (details == nil || (details.ownerID != "" && details.ownerID != r.Identity.ID)) {
					callErr = ErrInvalid
				}
				if callErr != nil {
					p.Details = cloneProjectDetails(prior)
					if p.Details == nil {
						p.Details = &ProjectDetails{Services: []ServiceDefinition{}}
					}
					p.Details.Error = failed(callErr)
					// Carry only same-project history details on a failed project observation.
					for j := range out.Deployments {
						d := &out.Deployments[j]
						if d.ProjectID == p.ID {
							old := previousDeployments[p.ID+"/"+d.ID]
							d.Details = cloneDeployment(old).Details
							if d.Details != nil {
								d.Details.Error = p.Details.Error
							}
						}
					}
					continue
				}
				p.Details = details
				// Preserve only identity-matching historical details; never copy a previous
				// deployment's services/aliases into a newly observed deployment.
				latestPreview := -1
				productionIndex := -1
				for j := range out.Deployments {
					d := &out.Deployments[j]
					if d.ProjectID != p.ID {
						continue
					}
					old := previousDeployments[p.ID+"/"+d.ID]
					d.Details = cloneDeployment(old).Details
					if d.ID == details.ProductionDeploymentID {
						productionIndex = j
					}
					if d.Target == "preview" && (latestPreview < 0 || d.CreatedAt.After(out.Deployments[latestPreview].CreatedAt)) {
						latestPreview = j
					}
				}
				if id := details.ProductionDeploymentID; id != "" {
					var old *Deployment
					if prior != nil && prior.ProductionDeploymentID == id && prior.ProductionDeployment != nil {
						old = prior.ProductionDeployment
					}
					if old == nil {
						if d, exists := previousDeployments[p.ID+"/"+id]; exists {
							old = &d
						}
					}
					if old == nil && productionIndex >= 0 {
						d := out.Deployments[productionIndex]
						old = &d
					}
					current := getDeployment(p.ID, id, old)
					getAliases(current, old)
					details.ProductionDeployment = current
					if productionIndex >= 0 {
						out.Deployments[productionIndex].Details = current.Details
					}
				}
				if latestPreview >= 0 {
					d := &out.Deployments[latestPreview]
					if d.ID == details.ProductionDeploymentID && details.ProductionDeployment != nil {
						d.Details = details.ProductionDeployment.Details
					} else {
						prior := *d
						d.Details = getDeployment(p.ID, d.ID, &prior).Details
					}
				}
			}
		}()
	}
	for i := range out.Projects {
		jobs <- i
	}
	close(jobs)
	workers.Wait()
	return cooldown
}
